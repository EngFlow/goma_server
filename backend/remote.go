// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"
	"crypto/tls"
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	"go.opencensus.io/plugin/ocgrpc"
	bspb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	pb "go.chromium.org/goma/server/proto/backend"
	execpb "go.chromium.org/goma/server/proto/exec"
	execlogpb "go.chromium.org/goma/server/proto/execlog"
	filepb "go.chromium.org/goma/server/proto/file"
)

// FromRemoteBackend creates new GRPC from cfg.
// returned func would release resources associated with GRPC.
func FromRemoteBackend(ctx context.Context, cfg *pb.RemoteBackend, opt Option) (GRPC, func(), error) {
	conn, err := grpc.DialContext(ctx, cfg.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})),
		grpc.WithStatsHandler(&ocgrpc.ClientHandler{}),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time: 10 * time.Second,
		}))
	if err != nil {
		return GRPC{}, func() {}, err
	}
	var apiKey []byte
	if cfg.ApiKeyName != "" {
		apiKey, err = ioutil.ReadFile(filepath.Join(opt.APIKeyDir, cfg.ApiKeyName))
		if err != nil {
			return GRPC{}, func() { conn.Close() }, err
		}
	}
	be := GRPC{
		ExecServer: ExecServer{
			Client: execpb.NewExecServiceClient(conn),
		},
		FileServer: FileServer{
			Client: filepb.NewFileServiceClient(conn),
		},
		ExeclogServer: ExeclogServer{
			Client: execlogpb.NewLogServiceClient(conn),
		},
		// TODO: propagate metadata.
		ByteStreamClient: bspb.NewByteStreamClient(conn),
		Auth:             opt.Auth,
		APIKey:           strings.TrimSpace(string(apiKey)),
	}
	return be, func() { conn.Close() }, nil
}
