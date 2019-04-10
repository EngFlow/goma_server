// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Binary file_server provides goma file service via gRPC.

*/
package main

import (
	"context"
	"flag"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
	"google.golang.org/grpc"

	"go.chromium.org/goma/server/cache"
	"go.chromium.org/goma/server/cache/gcs"
	"go.chromium.org/goma/server/cache/redis"
	"go.chromium.org/goma/server/file"
	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/profiler"
	"go.chromium.org/goma/server/server"

	cachepb "go.chromium.org/goma/server/proto/cache"
	pb "go.chromium.org/goma/server/proto/file"
)

var (
	port      = flag.Int("port", 5050, "rpc port")
	mport     = flag.Int("mport", 8081, "monitor port")
	cacheAddr = flag.String("file-cache-addr", "", "cache server address")
	bucket    = flag.String("bucket", "", "backing store bucket")

	traceProjectID     = flag.String("trace-project-id", "", "project id for cloud tracing")
	serviceAccountFile = flag.String("service-account-file", "", "service account json file")
)

func main() {
	flag.Parse()

	ctx := context.Background()

	profiler.Setup(ctx)

	logger := log.FromContext(ctx)
	defer logger.Sync()

	err := server.Init(ctx, *traceProjectID, "file_server")
	if err != nil {
		logger.Fatal(err)
	}

	s, err := server.NewGRPC(*port,
		grpc.MaxSendMsgSize(file.DefaultMaxMsgSize),
		grpc.MaxRecvMsgSize(file.DefaultMaxMsgSize))
	if err != nil {
		logger.Fatal(err)
	}

	var cclient cachepb.CacheServiceClient
	addr, err := redis.AddrFromEnv()
	switch {
	case err == nil:
		logger.Infof("redis enabled for gomafile: %s", addr)
		c := redis.NewClient(ctx, addr, "gomafile:")
		defer c.Close()
		cclient = c

	case *cacheAddr != "":
		logger.Infof("use cache server: %s", *cacheAddr)
		c := cache.NewClient(ctx, *cacheAddr,
			append([]grpc.DialOption{
				grpc.WithDefaultCallOptions(grpc.FailFast(false)),
			}, server.DefaultDialOption()...)...)
		defer c.Close()
		cclient = c

	case *bucket != "":
		logger.Infof("use cloud storage bucket: %s", *bucket)
		var opts []option.ClientOption
		if *serviceAccountFile != "" {
			opts = append(opts, option.WithServiceAccountFile(*serviceAccountFile))
		}
		gsclient, err := storage.NewClient(ctx, opts...)
		if err != nil {
			logger.Fatalf("storage client failed: %v", err)
		}
		defer gsclient.Close()
		c := gcs.New(gsclient.Bucket(*bucket))
		cclient = cache.LocalClient{CacheServiceServer: c}

	default:
		logger.Fatal("no cache server")
	}
	fs := &file.Service{
		Cache: cclient,
	}
	pb.RegisterFileServiceServer(s.Server, fs)
	hs := server.NewHTTP(*mport, nil)
	server.Run(ctx, s, hs)
}
