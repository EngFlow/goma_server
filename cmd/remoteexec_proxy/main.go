// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Binary remoteexec-proxy is a proxy server between Goma client and Remote Execution API.

*/
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/golang/protobuf/proto"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/trace"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"go.chromium.org/goma/server/auth"
	"go.chromium.org/goma/server/auth/account"
	"go.chromium.org/goma/server/auth/acl"
	"go.chromium.org/goma/server/cache"
	"go.chromium.org/goma/server/cache/gcs"
	"go.chromium.org/goma/server/cache/redis"
	"go.chromium.org/goma/server/file"
	"go.chromium.org/goma/server/frontend"
	"go.chromium.org/goma/server/httprpc"
	execrpc "go.chromium.org/goma/server/httprpc/exec"
	execlogrpc "go.chromium.org/goma/server/httprpc/execlog"
	filerpc "go.chromium.org/goma/server/httprpc/file"
	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/profiler"
	gomapb "go.chromium.org/goma/server/proto/api"
	authpb "go.chromium.org/goma/server/proto/auth"
	cachepb "go.chromium.org/goma/server/proto/cache"
	cmdpb "go.chromium.org/goma/server/proto/command"
	execpb "go.chromium.org/goma/server/proto/exec"
	filepb "go.chromium.org/goma/server/proto/file"
	"go.chromium.org/goma/server/remoteexec"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/rpc"
	"go.chromium.org/goma/server/server"
)

var (
	port = flag.Int("port", 8090, "listening port (goma api endpoints)")

	remoteexecAddr         = flag.String("remoteexec-addr", "", "remoteexec API endpoint")
	remoteInstanceName     = flag.String("remote-instance-name", "", "remote instance name")
	whitelistedUsers       = flag.String("whitelisted-users", "", "comma separated list of whitelisted user. if empty, current user is allowed.")
	serviceAccountJSON     = flag.String("service-account-json", "", "service account json, used to talk to RBE and cloud storage (if --file-cache-bucket is used)")
	platformContainerImage = flag.String("platform-container-image", "", "docker uri of platform container image")
	insecureRemoteexec     = flag.Bool("insecure-remoteexec", false, "insecure grpc for remoteexec API")

	fileCacheBucket = flag.String("file-cache-bucket", "", "file cache bucking store bucket")

	execConfigFile = flag.String("exec-config-file", "", "exec inventory config file")

	traceProjectID = flag.String("trace-project-id", "", "project id for cloud tracing")
	traceFraction  = flag.Float64("trace-sampling-fraction", 1.0, "sampling fraction for stackdriver trace")
	traceQPS       = flag.Float64("trace-sampling-qps-limit", 1.0, "sampling qps limit for stackdriver trace")
)

func myEmail(ctx context.Context) string {
	logger := log.FromContext(ctx)
	username := os.Getenv("USER")
	if username == "" {
		u, err := user.Current()
		if err != nil {
			logger.Fatalf("failed to get username: need --whitelisted-users: %v", err)
		}
		username = u.Username
	}
	buf, err := ioutil.ReadFile("/etc/mailname")
	if err != nil {
		logger.Fatalf("failed to get email: need --whiltelisted-users: %v", err)
	}
	return fmt.Sprintf("%s@%s", username, strings.TrimSpace(string(buf)))
}

type authClient struct {
	Service *auth.Service
}

func (c authClient) Auth(ctx context.Context, req *authpb.AuthReq, opts ...grpc.CallOption) (*authpb.AuthResp, error) {
	return c.Service.Auth(ctx, req)
}

type fileClient struct {
	Service filepb.FileServiceServer
}

func (c fileClient) StoreFile(ctx context.Context, req *gomapb.StoreFileReq, opts ...grpc.CallOption) (*gomapb.StoreFileResp, error) {
	return c.Service.StoreFile(ctx, req)
}

func (c fileClient) LookupFile(ctx context.Context, req *gomapb.LookupFileReq, opts ...grpc.CallOption) (*gomapb.LookupFileResp, error) {
	return c.Service.LookupFile(ctx, req)
}

type execlogService struct{}

func (c execlogService) SaveLog(ctx context.Context, req *gomapb.SaveLogReq) (*gomapb.SaveLogResp, error) {
	return &gomapb.SaveLogResp{}, nil
}

type cacheClient struct {
	Service cachepb.CacheServiceServer
}

func (c cacheClient) Get(ctx context.Context, req *cachepb.GetReq, opts ...grpc.CallOption) (*cachepb.GetResp, error) {
	return c.Service.Get(ctx, req)
}

func (c cacheClient) Put(ctx context.Context, req *cachepb.PutReq, opts ...grpc.CallOption) (*cachepb.PutResp, error) {
	return c.Service.Put(ctx, req)
}

const gomaClientClientID = "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com"

type defaultACL struct {
	whitelistedUser []string
}

func (a defaultACL) Load(ctx context.Context) (*authpb.ACL, error) {
	serviceAccount := "default"
	if *serviceAccountJSON != "" {
		serviceAccount = strings.TrimSuffix(filepath.Base(*serviceAccountJSON), ".json")
	}

	return &authpb.ACL{
		Groups: []*authpb.Group{
			{
				Id:             "user",
				Audience:       gomaClientClientID,
				Emails:         a.whitelistedUser,
				ServiceAccount: serviceAccount,
			},
		},
	}, nil
}

type reExecServer struct {
	re *remoteexec.Adapter
}

func (r reExecServer) Exec(ctx context.Context, req *gomapb.ExecReq) (*gomapb.ExecResp, error) {
	ctx, id := rpc.TagID(ctx, req.GetRequesterInfo())
	logger := log.FromContext(ctx)
	logger.Infof("call exec %s", id)
	return r.re.Exec(ctx, req)
}

type reFileServer struct {
	s filepb.FileServiceServer
}

func (r reFileServer) StoreFile(ctx context.Context, req *gomapb.StoreFileReq) (*gomapb.StoreFileResp, error) {
	ctx, id := rpc.TagID(ctx, req.GetRequesterInfo())
	logger := log.FromContext(ctx)
	logger.Infof("call storefile %s", id)
	return r.s.StoreFile(ctx, req)
}

func (r reFileServer) LookupFile(ctx context.Context, req *gomapb.LookupFileReq) (*gomapb.LookupFileResp, error) {
	ctx, id := rpc.TagID(ctx, req.GetRequesterInfo())
	logger := log.FromContext(ctx)
	logger.Infof("call lookupfile %s", id)
	return r.s.LookupFile(ctx, req)
}

type localBackend struct {
	ExecService execpb.ExecServiceServer
	FileService filepb.FileServiceServer
	Auth        httprpc.Auth
}

func (b localBackend) Ping() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		logger := log.FromContext(ctx)
		ctx, err := b.Auth.Auth(ctx, req)
		if err != nil {
			http.Error(w, fmt.Sprintf("auth failed: %v", err), http.StatusUnauthorized)
			logger.Errorf("ping unauthorized: %v", err)
			return
		}
		w.Header().Set("Accept-Encoding", "gzip, deflate")
		fmt.Fprintln(w, "ok")
	})
}

func (b localBackend) Exec() http.Handler {
	return execrpc.Handler(b.ExecService, httprpc.Timeout(5*time.Minute), httprpc.WithAuth(b.Auth))
}

func (b localBackend) ByteStream() http.Handler {
	return http.HandlerFunc(http.NotFound)
}

func (b localBackend) StoreFile() http.Handler {
	return filerpc.StoreHandler(b.FileService, httprpc.Timeout(1.*time.Minute), httprpc.WithAuth(b.Auth))
}

func (b localBackend) LookupFile() http.Handler {
	return filerpc.LookupHandler(b.FileService, httprpc.Timeout(1*time.Minute), httprpc.WithAuth(b.Auth))
}

func (b localBackend) Execlog() http.Handler {
	return execlogrpc.Handler(execlogService{}, httprpc.Timeout(1*time.Minute), httprpc.WithAuth(b.Auth))
}

func readConfigResp(fname string) (*cmdpb.ConfigResp, error) {
	b, err := ioutil.ReadFile(fname)
	if err != nil {
		return nil, err
	}
	resp := &cmdpb.ConfigResp{}
	err = proto.UnmarshalText(string(b), resp)
	if err != nil {
		return nil, err
	}
	// fix target address etc.
	for _, c := range resp.Configs {
		if c.Target == nil {
			c.Target = &cmdpb.Target{}
		}
		c.Target.Addr = *remoteexecAddr
		if c.BuildInfo == nil {
			c.BuildInfo = &cmdpb.BuildInfo{}
		}
	}
	return resp, nil
}

func main() {
	flag.Parse()
	ctx := context.Background()

	profiler.Setup(ctx)

	logger := log.FromContext(ctx)
	defer logger.Sync()

	if *whitelistedUsers == "" {
		*whitelistedUsers = myEmail(ctx)
	}
	var whitelisted []string
	for _, u := range strings.Split(*whitelistedUsers, ",") {
		whitelisted = append(whitelisted, strings.TrimSpace(u))
	}
	logger.Infof("allow access for %q", whitelisted)

	err := server.Init(ctx, *traceProjectID, "remoteexec-proxy")
	if err != nil {
		logger.Fatal(err)
	}

	trace.ApplyConfig(trace.Config{
		DefaultSampler: server.NewLimitedSampler(*traceFraction, *traceQPS),
	})

	saDir := "/"
	if *serviceAccountJSON != "" {
		logger.Infof("using service account: %s", *serviceAccountJSON)
		saDir = filepath.Dir(*serviceAccountJSON)
	} else {
		logger.Infof("using default service account")
	}
	aclCheck := acl.ACL{
		Loader: defaultACL{
			whitelistedUser: whitelisted,
		},
		Checker: acl.Checker{
			Pool: account.JSONDir{
				Dir: saDir,
				Scopes: []string{
					"https://www.googleapis.com/auth/cloud-build-service",
				},
			},
		},
	}
	err = aclCheck.Update(ctx)
	if err != nil {
		logger.Fatal(err)
	}

	authService := &auth.Service{
		CheckToken: aclCheck.CheckToken,
	}

	var cclient cachepb.CacheServiceClient
	if *fileCacheBucket != "" {
		logger.Infof("use cloud storage bucket: %s", *fileCacheBucket)
		var opts []option.ClientOption
		if *serviceAccountJSON != "" {
			opts = append(opts, option.WithServiceAccountFile(*serviceAccountJSON))
		}
		gsclient, err := storage.NewClient(ctx, opts...)
		if err != nil {
			logger.Fatalf("storage client failed: %v", err)
		}
		defer gsclient.Close()
		cclient = cache.LocalClient{
			CacheServiceServer: gcs.New(gsclient.Bucket(*fileCacheBucket)),
		}
	} else {
		cacheService, err := cache.New(cache.Config{
			MaxBytes: 1 * 1024 * 1024 * 1024,
		})
		if err != nil {
			logger.Fatal(err)
		}
		cclient = cacheClient{
			Service: cacheService,
		}
	}

	fileServiceClient := fileClient{
		Service: &file.Service{
			Cache: cclient,
		},
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})),
		grpc.WithStatsHandler(&ocgrpc.ClientHandler{}),
	}
	if *insecureRemoteexec {
		opts[0] = grpc.WithInsecure()
		logger.Warnf("use insecrure remoteexec API")
	}

	reConn, err := grpc.DialContext(ctx, *remoteexecAddr, opts...)
	if err != nil {
		logger.Fatal(err)
	}
	defer reConn.Close()

	var digestCache remoteexec.DigestCache
	redisAddr, err := redis.AddrFromEnv()
	if err != nil {
		logger.Warnf("redis disabled for gomafile-digest: %v", err)
		digestCache = digest.NewCache(nil)
	} else {
		logger.Infof("redis enabled for gomafile-digest: %v", redisAddr)
		digestCache = digest.NewCache(redis.NewClient(ctx, redisAddr, "gomafile-digest:"))
	}

	re := &remoteexec.Adapter{
		InstancePrefix: path.Dir(*remoteInstanceName),
		ExecTimeout:    15 * time.Minute,
		Client: remoteexec.Client{
			ClientConn: reConn,
		},
		InsecureClient: *insecureRemoteexec,
		GomaFile:       fileServiceClient,
		DigestCache:    digestCache,
		ToolDetails: &rpb.ToolDetails{
			ToolName:    "remoteexec_proxy",
			ToolVersion: "0.0.0-experimental",
		},
		FileLookupConcurrency: 2,
	}

	configResp := &cmdpb.ConfigResp{
		VersionId: time.Now().UTC().Format(time.RFC3339),
		Configs: []*cmdpb.Config{
			{
				Target: &cmdpb.Target{
					Addr: *remoteexecAddr,
				},
				BuildInfo: &cmdpb.BuildInfo{},
				Dimensions: []string{
					"os:linux",
				},
				RemoteexecPlatform: &cmdpb.RemoteexecPlatform{
					RbeInstanceBasename: path.Base(*remoteInstanceName),
					Properties: []*cmdpb.RemoteexecPlatform_Property{
						{
							Name:  "container-image",
							Value: *platformContainerImage,
						}, {
							Name:  "OSFamily",
							Value: "Linux",
						},
					},
				},
			},
		},
	}
	// TODO: document config example?
	if *execConfigFile != "" {
		c, err := readConfigResp(*execConfigFile)
		if err != nil {
			logger.Fatal(err)
		}
		configResp = c
	}
	err = re.Inventory.Configure(ctx, configResp)
	if err != nil {
		logger.Fatal(err)
	}
	mux := http.DefaultServeMux
	frontend.Register(mux, frontend.Frontend{
		Backend: localBackend{
			ExecService: reExecServer{re},
			FileService: reFileServer{fileServiceClient.Service},
			Auth: &auth.Auth{
				Client: authClient{Service: authService},
			},
		},
	})

	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintln(w, "ok")
	}))
	tmpl := template.Must(template.New("index").Parse(`
<html>
<head>
 <title>Goma remoteexec_proxy at {{.Port}}</title>
</head>
<body>
<h1>Goma remoteexec_proxy</h1>

<p><b>remoteexec-addr:</b> {{.RemoteexecAddr}}</p>
<p><b>remote-instance-name:</b> {{.RemoteInstanceName}}</p>
<p><b>whitelisted-users:</b> {{.WhitelistedUsers}}</p>
<p><b>service-account-json:</b> <a href="file://{{.ServiceAccountJSON}}">{{.ServiceAccountJSON}}</a></p>
<p><b>platform-container-image:</b> {{.PlatformContainerImage}}</p>
<p><b>redis:</b> {{.RedisAddr}}</p>
<p><b>file-cache-bucket:</b> {{.FileCacheBucket}}</p>

<p><b>config:</b>
<pre>{{.Config}}</pre>

<hr>
<p>
<a href="/debug/tracez">/debug/tracez</a> |
<a href="/debug/rpcz">/debug/rpcz</a> |
<a href="/healthz">/healthz - for health check</a>
</body>
</html>`))

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		err := tmpl.Execute(w, struct {
			Port                   int
			RemoteexecAddr         string
			RemoteInstanceName     string
			WhitelistedUsers       []string
			ServiceAccountJSON     string
			PlatformContainerImage string
			RedisAddr              string
			FileCacheBucket        string
			Config                 *cmdpb.ConfigResp
		}{
			Port:                   *port,
			RemoteexecAddr:         *remoteexecAddr,
			RemoteInstanceName:     *remoteInstanceName,
			WhitelistedUsers:       whitelisted,
			ServiceAccountJSON:     *serviceAccountJSON,
			PlatformContainerImage: *platformContainerImage,
			RedisAddr:              redisAddr,
			FileCacheBucket:        *fileCacheBucket,
			Config:                 configResp,
		})
		if err != nil {
			logger := log.FromContext(ctx)
			logger.Errorf("index template: %v", err)
		}
	}))
	hsMain := server.NewHTTP(*port, mux)
	server.Run(ctx, hsMain)
}
