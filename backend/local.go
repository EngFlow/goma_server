// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"
	"fmt"

	bspb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"

	"go.chromium.org/goma/server/exec"
	"go.chromium.org/goma/server/execlog"
	"go.chromium.org/goma/server/file"
	pb "go.chromium.org/goma/server/proto/backend"
	filepb "go.chromium.org/goma/server/proto/file"
	"go.chromium.org/goma/server/server"
)

// FromLocalBackend creates new GRPC from cfg.
// returned func would release resources associated with GRPC.
func FromLocalBackend(ctx context.Context, cfg *pb.LocalBackend, opt Option) (GRPC, func(), error) {
	fileAddr := cfg.FileAddr
	if fileAddr == "" {
		fileAddr = "file-server:5050"
	}
	fileConn, err := server.DialContext(ctx, fileAddr,
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(file.DefaultMaxMsgSize),
			grpc.MaxCallRecvMsgSize(file.DefaultMaxMsgSize),
			grpc.FailFast(false)))
	if err != nil {
		return GRPC{}, func() {}, fmt.Errorf("dial %s: %v", fileAddr, err)
	}
	execAddr := cfg.ExecAddr
	if execAddr == "" {
		execAddr = "exec-server:5050"
	}
	var bsConn *grpc.ClientConn
	var bsClient bspb.ByteStreamClient
	if cfg.EnableBytestream {
		bsConn, err = server.DialContext(ctx, execAddr)
		if err != nil {
			fileConn.Close()
			return GRPC{}, func() {}, fmt.Errorf("dial %s: %v", execAddr, err)
		}
		bsClient = bspb.NewByteStreamClient(bsConn)
	}
	dialOptions := append(
		[]grpc.DialOption{
			grpc.WithDefaultCallOptions(grpc.FailFast(false)),
		},
		server.DefaultDialOption()...)
	execlogAddr := cfg.ExeclogAddr
	if execlogAddr == "" {
		execlogAddr = "execlog-server:5050"
	}
	be := GRPC{
		ExecServer: ExecServer{
			Client: exec.NewClient(execAddr, dialOptions...),
		},
		FileServer: FileServer{
			Client: filepb.NewFileServiceClient(fileConn),
		},
		ExeclogServer: ExeclogServer{
			Client: execlog.NewClient(execlogAddr, dialOptions...),
		},
		ByteStreamClient: bsClient,
		Auth:             opt.Auth,
	}
	if cfg.TraceOption != nil {
		be.Namespace = cfg.TraceOption.Namespace
		be.Cluster = cfg.TraceOption.Cluster
	}
	return be, func() {
		bsConn.Close()
		fileConn.Close()
	}, nil
}
