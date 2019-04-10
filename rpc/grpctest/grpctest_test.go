// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package grpctest_test

import (
	"context"
	"fmt"
	"os"

	"google.golang.org/grpc"

	gomapb "go.chromium.org/goma/server/proto/api"
	pb "go.chromium.org/goma/server/proto/execlog"
	"go.chromium.org/goma/server/rpc/grpctest"
)

// MyServer is fake execlog server.
type MyServer struct {
	Req  *gomapb.SaveLogReq
	Resp *gomapb.SaveLogResp
	Err  error
}

func (s *MyServer) SaveLog(ctx context.Context, req *gomapb.SaveLogReq) (*gomapb.SaveLogResp, error) {
	s.Req = req
	return s.Resp, s.Err
}

func Example() {
	srv := grpc.NewServer()
	s := &MyServer{
		Resp: &gomapb.SaveLogResp{},
	}
	pb.RegisterLogServiceServer(srv, s)
	addr, stop, err := grpctest.StartServer(srv)
	if err != nil {
		fmt.Printf("error creating test server: %v\n", err)
		os.Exit(1)
	}
	defer stop()

	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		fmt.Printf("error connecting to %s: %v\n", addr, err)
		os.Exit(1)
	}
	defer conn.Close()
	client := pb.NewLogServiceClient(conn)
	ctx := context.Background()
	resp, err := client.SaveLog(ctx, &gomapb.SaveLogReq{})
	if err != nil {
		fmt.Printf("SaveLog()=%v, %v; want nil error\n", resp, err)
		os.Exit(1)
	}
}
