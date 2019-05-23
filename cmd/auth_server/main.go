// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Binary auth_server provides auth service via gRPC.

*/
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"path/filepath"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"

	"go.chromium.org/goma/server/auth"
	"go.chromium.org/goma/server/auth/account"
	"go.chromium.org/goma/server/auth/acl"
	"go.chromium.org/goma/server/auth/authdb"
	"go.chromium.org/goma/server/fswatch"
	"go.chromium.org/goma/server/httprpc"
	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/log/errorreporter"
	"go.chromium.org/goma/server/profiler"
	pb "go.chromium.org/goma/server/proto/auth"
	"go.chromium.org/goma/server/remoteexec"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/rpc"
	"go.chromium.org/goma/server/server"
)

var (
	port  = flag.Int("port", 5050, "rpc port")
	mport = flag.Int("mport", 8081, "monitor port")

	projectID = flag.String("project-id", "", "project id")

	authDBAddr            = flag.String("auth-db-addr", "", "authdb url")
	aclFile               = flag.String("acl-file", "", "filename of acl proto text message")
	serviceAccountJSONDir = flag.String("service-account-json-dir", "", "directory for service account jsons")

	remoteexecAddr     = flag.String("remoteexec-addr", "", "use remoteexec API endpoint")
	remoteInstanceName = flag.String("remote-instance-name", "", "remote instance name.")
)

var (
	configUpdate = stats.Int64("go.chromium.org/goma/server/cmd/auth_server.acl-updates", "acl updates", stats.UnitDimensionless)

	configStatusKey = mustTagNewKey("status")

	configViews = []*view.View{
		{
			Description: "counts acl updates",
			TagKeys: []tag.Key{
				configStatusKey,
			},
			Measure:     configUpdate,
			Aggregation: view.Count(),
		},
	}
)

func mustTagNewKey(name string) tag.Key {
	k, err := tag.NewKey(name)
	if err != nil {
		logger := log.FromContext(context.Background())
		logger.Fatal(err)
	}
	return k
}

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

type tokenChecker struct {
	Client   remoteexec.Client
	Instance string
}

func (tc *tokenChecker) CheckToken(ctx context.Context, token *oauth2.Token, tokenInfo *auth.TokenInfo) (string, *oauth2.Token, error) {
	d := digest.Bytes("auth check", []byte("auth check"))
	err := rpc.Retry{}.Do(ctx, func() error {
		_, err := tc.Client.CAS().FindMissingBlobs(ctx, &rpb.FindMissingBlobsRequest{
			InstanceName: tc.Instance,
			BlobDigests: []*rpb.Digest{
				d.Digest(),
			},
		}, grpc.PerRPCCredentials(oauth.NewOauthAccess(token)))
		return err
	})
	if err != nil {
		return "", nil, err
	}
	return "", token, nil
}

func main() {
	flag.Parse()

	ctx := context.Background()

	profiler.Setup(ctx)

	logger := log.FromContext(ctx)
	defer logger.Sync()

	err := server.Init(ctx, *projectID, "auth_server")
	if err != nil {
		logger.Fatal(err)
	}

	err = view.Register(configViews...)
	if err != nil {
		logger.Fatal(err)
	}

	s, err := server.NewGRPC(*port)
	if err != nil {
		logger.Fatal(err)
	}
	var checkToken func(context.Context, *oauth2.Token, *auth.TokenInfo) (string, *oauth2.Token, error)
	if *remoteexecAddr != "" {
		logger.Infof("use remoteexec API: %s", *remoteexecAddr)
		reConn, err := grpc.DialContext(ctx, *remoteexecAddr,
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})),
			grpc.WithStatsHandler(&ocgrpc.ClientHandler{}))
		if err != nil {
			logger.Fatalf("dial %s: %v", *remoteexecAddr, err)
		}
		defer reConn.Close()
		if *remoteInstanceName == "" {
			logger.Fatalf("--remote-instance-name must be given for remoteexec API")
		}
		tc := &tokenChecker{
			Client: remoteexec.Client{
				ClientConn: reConn,
			},
			Instance: *remoteInstanceName,
		}
		checkToken = tc.CheckToken
	}

	if *aclFile != "" {
		var authDB acl.AuthDB
		if *authDBAddr != "" {
			authDB = authdb.Client{
				Client: &httprpc.Client{
					URL: *authDBAddr,
				},
			}
			logger.Infof("use authdb: %s", *authDBAddr)
		}
		if *serviceAccountJSONDir == "" {
			logger.Fatalf("--service-account-json-dir must be given for acl")
		}
		a := acl.ACL{
			Loader: acl.FileLoader{
				Filename: *aclFile,
			},
			Checker: acl.Checker{
				AuthDB: authDB,
				Pool: account.JSONDir{
					Dir: *serviceAccountJSONDir,
					Scopes: []string{
						"https://www.googleapis.com/auth/cloud-build-service",
					},
				},
			},
		}
		err := a.Update(ctx)
		if err != nil {
			recordConfigUpdate(ctx, err)
			logger.Fatalf("acl update failed: %v", err)
		}
		recordConfigUpdate(ctx, nil)
		go func() {
			defer errorreporter.Do(nil, nil)
			ctx := context.Background()
			logger := log.FromContext(ctx)
			watcher, err := fswatch.New(ctx, filepath.Dir(*aclFile))
			if err != nil {
				logger.Fatalf("fswatch failed: %v", err)
			}
			defer watcher.Close()
			for {
				logger.Infof("waiting for acl update...")
				ev, err := watcher.Next(ctx)
				if err != nil {
					logger.Fatalf("watch failed: %v", err)
				}
				logger.Infof("acl update: %v", ev)
				err = a.Update(ctx)
				if err != nil {
					recordConfigUpdate(ctx, err)
					logger.Errorf("acl update failed: %v", err)
					continue
				}
				logger.Infof("acl updated")
				recordConfigUpdate(ctx, nil)
			}
		}()
		rbeCheckToken := checkToken
		checkToken = func(ctx context.Context, token *oauth2.Token, tokenInfo *auth.TokenInfo) (string, *oauth2.Token, error) {
			account, token, err := a.CheckToken(ctx, token, tokenInfo)
			if err != nil {
				return "", nil, err
			}
			if rbeCheckToken != nil {
				_, token, err = rbeCheckToken(ctx, token, tokenInfo)
				return account, token, err
			}
			return account, token, nil
		}
		logger.Infof("acl configured")
	}

	if checkToken == nil {
		var a acl.ACL
		err := a.Update(ctx)
		if err != nil {
			logger.Fatalf("acl update failed: %v", err)
		}
		checkToken = a.CheckToken
	}

	as := &auth.Service{
		CheckToken: checkToken,
	}
	pb.RegisterAuthServiceServer(s.Server, as)

	hs := server.NewHTTP(*mport, nil)

	server.Run(ctx, s, hs)
}
