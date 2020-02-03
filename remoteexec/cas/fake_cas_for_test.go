// Copyright 2020 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package cas

import (
	"fmt"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/fakes"
	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"

	"go.chromium.org/goma/server/rpc/grpctest"
)

var (
	errNotImplemented = fmt.Errorf("Function not implemented.")
)

type fakeCASServer struct {
	srv *grpc.Server
	cas *fakes.CAS

	addr string
	stop func()
}

func newFakeCASServer() (*fakeCASServer, error) {
	f := &fakeCASServer{
		srv: grpc.NewServer(),
		cas: fakes.NewCAS(),
	}
	bpb.RegisterByteStreamServer(f.srv, f.cas)
	rpb.RegisterContentAddressableStorageServer(f.srv, f.cas)
	var err error
	f.addr, f.stop, err = grpctest.StartServer(f.srv)
	if err != nil {
		f.stop()
		return nil, err
	}
	return f, nil
}

type fakeCASClient struct {
	Client
	casClient rpb.ContentAddressableStorageClient
	bsClient  bpb.ByteStreamClient

	batchUpdateByteLimit int64
	server               *fakeCASServer
	conn                 *grpc.ClientConn
}

func (f *fakeCASClient) CAS() rpb.ContentAddressableStorageClient {
	return f.casClient
}

func (f *fakeCASClient) ByteStream() bpb.ByteStreamClient {
	return f.bsClient
}

func (f *fakeCASClient) teardown() {
	f.conn.Close()
	f.server.stop()
}

func newFakeCASClient(byteLimit int64, instances ...string) (*fakeCASClient, error) {
	if byteLimit == 0 {
		byteLimit = DefaultBatchByteLimit
	}

	f := &fakeCASClient{
		batchUpdateByteLimit: byteLimit,
	}
	var err error

	f.server, err = newFakeCASServer()
	if err != nil {
		return nil, err
	}

	f.conn, err = grpc.Dial(f.server.addr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	f.bsClient = bpb.NewByteStreamClient(f.conn)
	f.casClient = rpb.NewContentAddressableStorageClient(f.conn)

	return f, nil
}
