// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Binary execlog_server provides goma execlog service via gRPC.

*/
package main

import (
	"context"
	"flag"
	"net/http"

	"go.opencensus.io/trace"
	"go.opencensus.io/zpages"
	"google.golang.org/grpc"

	"go.chromium.org/goma/server/execlog"
	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/profiler"
	pb "go.chromium.org/goma/server/proto/execlog"
	"go.chromium.org/goma/server/server"
)

var (
	port  = flag.Int("port", 5050, "rpc port")
	mport = flag.Int("mport", 8081, "monitor port")

	projectID = flag.String("project-id", "", "project id")
)

func main() {
	flag.Parse()

	ctx := context.Background()

	profiler.Setup(ctx)

	logger := log.FromContext(ctx)
	defer logger.Sync()

	err := server.Init(ctx, *projectID, "execlog_server")
	if err != nil {
		logger.Fatal(err)
	}
	trace.ApplyConfig(trace.Config{
		DefaultSampler: server.NewRemoteSampler(true, trace.NeverSample()),
	})

	s, err := server.NewGRPC(*port,
		grpc.MaxRecvMsgSize(execlog.DefaultMaxReqMsgSize))
	if err != nil {
		logger.Fatal(err)
	}
	els := &execlog.Service{}
	pb.RegisterLogServiceServer(s.Server, els)

	hs := server.NewHTTP(*mport, nil)
	zpages.Handle(http.DefaultServeMux, "/debug")
	server.Run(ctx, s, hs)
}
