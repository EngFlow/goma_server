// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Binary cache_server provides cache service via gRPC.

*/
package main

import (
	"context"
	"flag"
	"runtime/debug"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"

	"go.chromium.org/goma/server/cache"
	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/profiler"
	pb "go.chromium.org/goma/server/proto/cache"
	"go.chromium.org/goma/server/server"

	_ "expvar"
	_ "net/http/pprof"
)

var (
	port               = flag.Int("port", 5050, "rpc port")
	mport              = flag.Int("mport", 8081, "monitor port")
	bucket             = flag.String("bucket", "", "backing store bucket")
	serviceAccountFile = flag.String("service-account-file", "", "service account json file")
	// config = flag.String("config", "", "config file")

	traceProjectID = flag.String("trace-project-id", "", "project id for cloud tracing")
)

func main() {
	flag.Parse()

	ctx := context.Background()
	// Set low GC percent for better memory usage.
	debug.SetGCPercent(30)
	profiler.Setup(ctx)

	logger := log.FromContext(ctx)
	defer logger.Sync()

	err := server.Init(ctx, *traceProjectID, "cache_server")
	if err != nil {
		logger.Fatal(err)
	}

	var bucketHandle *storage.BucketHandle
	if *bucket != "" {
		var opts []option.ClientOption
		if *serviceAccountFile != "" {
			opts = append(opts, option.WithServiceAccountFile(*serviceAccountFile))
		}
		gsclient, err := storage.NewClient(ctx, opts...)
		if err != nil {
			logger.Fatalf("storage client failed: %v", err)
		}
		defer gsclient.Close()
		bucketHandle = gsclient.Bucket(*bucket)
	}

	s, err := server.NewGRPC(*port)
	if err != nil {
		logger.Fatal(err)
	}
	c, err := cache.New(cache.Config{
		MaxBytes: 1 * 1024 * 1024 * 1024,
		Bucket:   bucketHandle,
	})
	if err != nil {
		logger.Fatalf("failed to create cache client: %v", err)
	}
	pb.RegisterCacheServiceServer(s.Server, c)

	hs := server.NewHTTP(*mport, nil)
	server.Run(ctx, s, hs)
}
