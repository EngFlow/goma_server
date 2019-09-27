// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Binary exec_server provides goma exec service via gRPC.

*/
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"path"
	"time"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/storage"
	"github.com/golang/protobuf/proto"
	"github.com/googleapis/google-cloud-go-testing/storage/stiface"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/zpages"
	"google.golang.org/api/option"
	bspb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"go.chromium.org/goma/server/cache/redis"
	"go.chromium.org/goma/server/command"
	"go.chromium.org/goma/server/exec"
	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/log/errorreporter"
	"go.chromium.org/goma/server/profiler"
	cmdpb "go.chromium.org/goma/server/proto/command"
	pb "go.chromium.org/goma/server/proto/exec"
	filepb "go.chromium.org/goma/server/proto/file"
	"go.chromium.org/goma/server/remoteexec"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/server"
)

var (
	port         = flag.Int("port", 5050, "rpc port")
	mport        = flag.Int("mport", 8081, "monitor port")
	fileAddr     = flag.String("file-addr", "passthrough:///file-server:5050", "file server address")
	configMapURI = flag.String("configmap_uri", "", "configmap uri. e.g. gs://$project-toolchain-config/$name.config, text proto of command.ConfigMap.")
	configMap    = flag.String("configmap", "", "configmap text proto")

	traceProjectID     = flag.String("trace-project-id", "", "project id for cloud tracing")
	pubsubProjectID    = flag.String("pubsub-project-id", "", "project id for pubsub")
	serviceAccountFile = flag.String("service-account-file", "", "service account json file")

	remoteexecAddr       = flag.String("remoteexec-addr", "", "use remoteexec API endpoint")
	remoteInstancePrefix = flag.String("remote-instance-prefix", "", "remote instance name path prefix.")
	cmdFilesBucket       = flag.String("cmd-files-bucket", "", "cloud storage bucket for command binary files")

	// Needed for b/120582303, but will be deprecated by b/80508682.
	fileLookupConcurrency = flag.Int("file-lookup-concurrency", 5, "concurrency to look up files from file-server")
)

var (
	configUpdate = stats.Int64("go.chromium.org/goma/server/cmd/exec_server.toolchain-config-updates", "toolchain-config updates", stats.UnitDimensionless)

	configStatusKey = tag.MustNewKey("status")

	configViews = []*view.View{
		{
			Description: "counts toolchain-config updates",
			TagKeys: []tag.Key{
				configStatusKey,
			},
			Measure:     configUpdate,
			Aggregation: view.Count(),
		},
	}
)

func recordConfigUpdate(ctx context.Context, err error) {
	logger := log.FromContext(ctx)
	status := "success"
	if err != nil {
		status = "failure"
	}
	ctx, cerr := tag.New(ctx, tag.Upsert(configStatusKey, status))
	if cerr != nil {
		logger.Fatal(cerr)
	}
	stats.Record(ctx, configUpdate.M(1))
	if err != nil {
		server.Flush()
	}
}

func configMapToConfigResp(ctx context.Context, cm *cmdpb.ConfigMap) *cmdpb.ConfigResp {
	resp := &cmdpb.ConfigResp{
		VersionId: time.Now().UTC().Format(time.RFC3339),
	}
	for _, rt := range cm.Runtimes {
		c := &cmdpb.Config{
			Target: &cmdpb.Target{
				Addr: *remoteexecAddr,
			},
			BuildInfo:          &cmdpb.BuildInfo{},
			RemoteexecPlatform: &cmdpb.RemoteexecPlatform{},
			Dimensions:         rt.PlatformRuntimeConfig.Dimensions,
		}
		for _, p := range rt.Platform.Properties {
			c.RemoteexecPlatform.Properties = append(c.RemoteexecPlatform.Properties, &cmdpb.RemoteexecPlatform_Property{
				Name:  p.Name,
				Value: p.Value,
			})
		}
		resp.Configs = append(resp.Configs, c)
	}
	return resp
}

func configureByLoader(ctx context.Context, loader *command.ConfigMapLoader, inventory *exec.Inventory) (string, error) {
	resp, err := loader.Load(ctx)
	if err != nil {
		return "", err
	}
	err = inventory.Configure(ctx, resp)
	if err != nil {
		return "", err
	}
	return resp.VersionId, nil
}

type cmdStorageBucket struct {
	Bucket *storage.BucketHandle
}

func (b cmdStorageBucket) Open(ctx context.Context, hash string) (io.ReadCloser, error) {
	return b.Bucket.Object(path.Join("sha256", hash)).NewReader(ctx)
}

type nullServer struct {
	ch chan error
}

func (s nullServer) ListenAndServe() error { return <-s.ch }
func (s nullServer) Shutdown(ctx context.Context) error {
	close(s.ch)
	return nil
}

type configServer struct {
	inventory *exec.Inventory
	configmap command.ConfigMap
	psclient  *pubsub.Client
	w         command.ConfigMapWatcher
	loader    *command.ConfigMapLoader
	cancel    func()
}

func newConfigServer(ctx context.Context, inventory *exec.Inventory, uri string, gsclient *storage.Client, opts ...option.ClientOption) (*configServer, error) {
	cs := &configServer{
		inventory: inventory,
	}
	if *pubsubProjectID == "" {
		*pubsubProjectID = server.ProjectID(ctx)
		if *pubsubProjectID == "" {
			return nil, errors.New("--pubsub-project-id must be set")
		}
	}
	var err error
	cs.psclient, err = pubsub.NewClient(ctx, *pubsubProjectID, opts...)
	if err != nil {
		return nil, fmt.Errorf("pubsub client failed: %v", err)
	}
	cs.configmap = command.ConfigMapBucket{
		URI:            uri,
		StorageClient:  stiface.AdaptClient(gsclient),
		PubsubClient:   cs.psclient,
		SubscriberID:   fmt.Sprintf("toolchain-config-%s-%s", server.ClusterName(ctx), server.HostName(ctx)),
		RemoteexecAddr: *remoteexecAddr,
	}
	cs.w = cs.configmap.Watcher(ctx)
	cs.loader = &command.ConfigMapLoader{
		ConfigMap: cs.configmap,
		ConfigLoader: command.ConfigLoader{
			StorageClient: stiface.AdaptClient(gsclient),
		},
	}
	return cs, nil
}

func (cs *configServer) configure(ctx context.Context) error {
	logger := log.FromContext(ctx)
	id, err := configureByLoader(ctx, cs.loader, cs.inventory)
	if err != nil {
		if err != command.ErrNoUpdate {
			recordConfigUpdate(ctx, err)
		}
		logger.Errorf("failed to configure: %v", err)
		return err
	}
	logger.Infof("configure %s", id)
	recordConfigUpdate(ctx, nil)
	return nil
}

func (cs *configServer) ListenAndServe() error {
	ctx, cancel := context.WithCancel(context.Background())
	cs.cancel = cancel
	logger := log.FromContext(ctx)
	for {
		logger.Infof("waiting for config update...")
		err := cs.w.Next(ctx)
		if err != nil {
			logger.Errorf("watch failed %v", err)
			return err
		}
		err = cs.configure(ctx)
		if err == command.ErrNoUpdate {
			continue
		}
		if err != nil {
			logger.Errorf("config failed: %v", err)
			continue
		}
	}
}

func (cs *configServer) Shutdown(ctx context.Context) error {
	defer errorreporter.Do(nil, nil)
	defer func() {
		if cs.psclient != nil {
			cs.psclient.Close()
		}
	}()
	if cs.cancel != nil {
		cs.cancel()
	}
	return cs.w.Close()
}

func newDigestCache(ctx context.Context) remoteexec.DigestCache {
	logger := log.FromContext(ctx)
	addr, err := redis.AddrFromEnv()
	if err != nil {
		logger.Warnf("redis disabled for gomafile-digest: %v", err)
		return digest.NewCache(nil)
	}
	logger.Infof("redis enabled for gomafile-digest: %v", addr)
	return digest.NewCache(redis.NewClient(ctx, addr, "gomafile-digest:"))
}

func main() {
	flag.Parse()
	rand.Seed(time.Now().UnixNano())

	ctx := context.Background()

	profiler.Setup(ctx)

	logger := log.FromContext(ctx)
	defer logger.Sync()

	if *configMapURI == "" && *configMap == "" {
		logger.Fatalf("--configmap_uri or --configmap must be given")
	}
	if *remoteexecAddr == "" {
		logger.Fatalf("--remoteexec-addr must be given")
	}

	err := server.Init(ctx, *traceProjectID, "exec_server")
	if err != nil {
		logger.Fatal(err)
	}

	err = view.Register(configViews...)
	if err != nil {
		logger.Fatal(err)
	}
	err = view.Register(command.DefaultViews...)
	if err != nil {
		logger.Fatal(err)
	}
	err = view.Register(exec.DefaultViews...)
	if err != nil {
		logger.Fatal(err)
	}
	err = view.Register(remoteexec.DefaultViews...)
	if err != nil {
		logger.Fatal(err)
	}

	s, err := server.NewGRPC(*port,
		grpc.MaxSendMsgSize(exec.DefaultMaxRespMsgSize),
		grpc.MaxRecvMsgSize(exec.DefaultMaxReqMsgSize))
	if err != nil {
		logger.Fatal(err)
	}

	fileConn, err := server.DialContext(ctx, *fileAddr)
	if err != nil {
		logger.Fatalf("dial %s: %v", *fileAddr, err)
	}
	defer fileConn.Close()

	var gsclient *storage.Client
	var opts []option.ClientOption
	if *configMapURI != "" || *cmdFilesBucket != "" {
		logger.Infof("configmap_uri or cmd-files-bucket is specified. use cloud storage")
		if *serviceAccountFile != "" {
			opts = append(opts, option.WithServiceAccountFile(*serviceAccountFile))
		}
		gsclient, err = storage.NewClient(ctx, opts...)
		if err != nil {
			logger.Fatalf("storage client failed: %v", err)
		}
		defer gsclient.Close()
	} else {
		logger.Infof("configmap_uri nor cmd-files-bucket is not specified. don't use cloud storage")
	}

	logger.Infof("use remoteexec API: %s", *remoteexecAddr)
	reConn, err := grpc.DialContext(ctx, *remoteexecAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})),
		grpc.WithStatsHandler(&ocgrpc.ClientHandler{}))
	if err != nil {
		logger.Fatalf("dial %s: %v", *remoteexecAddr, err)
	}
	defer reConn.Close()

	if *remoteInstancePrefix == "" {
		logger.Fatalf("--remote-instance-prefix must be given for remoteexec API")
	}

	re := &remoteexec.Adapter{
		InstancePrefix: *remoteInstancePrefix,
		ExecTimeout:    15 * time.Minute,
		Client: remoteexec.Client{
			ClientConn: reConn,
		},
		GomaFile:    filepb.NewFileServiceClient(fileConn),
		DigestCache: newDigestCache(ctx),
		ToolDetails: &rpb.ToolDetails{
			ToolName:    "goma/exec-server",
			ToolVersion: "0.0.0-experimental",
		},
		FileLookupConcurrency: *fileLookupConcurrency,
	}

	if *cmdFilesBucket == "" {
		logger.Warnf("--cmd-files-bucket is not given. support only ARBITRARY_TOOLCHAIN_SUPPORT enabled client")
	} else {
		logger.Infof("use gs://%s for cmd files", *cmdFilesBucket)
		re.CmdStorage = cmdStorageBucket{
			Bucket: gsclient.Bucket(*cmdFilesBucket),
		}
	}

	inventory := &re.Inventory

	// expose bytestream proxy.
	bs := &remoteexec.ByteStream{
		Adapter:      re,
		InstanceName: re.DefaultInstance(),
		// TODO: Create bytestreams for multiple instances.
	}
	bspb.RegisterByteStreamServer(s.Server, bs)

	var confServer server.Server
	ready := make(chan error)
	switch {
	case *configMap != "":
		go func() {
			cm := &cmdpb.ConfigMap{}
			err := proto.UnmarshalText(*configMap, cm)
			if err != nil {
				ready <- fmt.Errorf("parse configmap %q: %v", *configMap, err)
				return
			}
			resp := configMapToConfigResp(ctx, cm)
			err = inventory.Configure(ctx, resp)
			if err != nil {
				ready <- err
				return
			}
			logger.Infof("configure %s", resp.VersionId)
			ready <- nil
		}()
		confServer = nullServer{ch: make(chan error)}

	case *configMapURI != "":
		cs, err := newConfigServer(ctx, inventory, *configMapURI, gsclient, opts...)
		if err != nil {
			logger.Fatalf("configServer: %v", err)
		}
		go func() {
			ready <- cs.configure(ctx)
		}()
		confServer = cs
	}
	http.Handle("/configz", inventory)
	pb.RegisterExecServiceServer(s.Server, re)

	// as of Dec 14 2018, it takes about 45 seconds to be ready.
	// so wait 90-110 seconds with buffer.  b/120394151
	// assume initialDelaySeconds: 120.
	// TODO: split toolchain config server and exec server? b/120115232
	timeout := 90*time.Second + time.Duration(float64(20*time.Second)*rand.Float64())
	logger.Infof("wait %s to be ready", timeout)
	start := time.Now()
	select {
	case err = <-ready:
		if err != nil {
			logger.Errorf("configure: %v", err)
			confServer.Shutdown(ctx)
			server.Flush()
			logger.Fatalf("initial config failed: %v", err)
		}
		logger.Infof("exec-server ready in %s", time.Since(start))
	case <-time.After(timeout):
		logger.Errorf("initial loading timed out")
		confServer.Shutdown(ctx)
		server.Flush()
		logger.Fatalf("no configs available in %s", timeout)
	}
	hs := server.NewHTTP(*mport, nil)
	zpages.Handle(http.DefaultServeMux, "/debug")
	server.Run(ctx, s, hs, confServer)
}
