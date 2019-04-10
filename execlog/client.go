// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package execlog

import (
	"context"

	"google.golang.org/grpc"

	gomapb "go.chromium.org/goma/server/proto/api"
	pb "go.chromium.org/goma/server/proto/execlog"
)

// TODO: expvar counter

// Client is a client to access execlog service via gRPC.
type Client struct {
	addr     string
	dialOpts []grpc.DialOption
}

// NewClient creates new client to access execlog service serving on address.
func NewClient(address string, opts ...grpc.DialOption) Client {
	return Client{
		addr:     address,
		dialOpts: opts,
	}
}

// SaveLog saves requested execlog.
func (c Client) SaveLog(ctx context.Context, in *gomapb.SaveLogReq, opts ...grpc.CallOption) (*gomapb.SaveLogResp, error) {
	conn, err := grpc.DialContext(ctx, c.addr,
		append([]grpc.DialOption{
			grpc.WithBlock(),
		}, c.dialOpts...)...)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return pb.NewLogServiceClient(conn).SaveLog(ctx, in,
		append([]grpc.CallOption{
			grpc.MaxCallSendMsgSize(DefaultMaxReqMsgSize),
		}, opts...)...)
}
