// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package exec

import (
	"context"

	"go.opencensus.io/trace"
	"google.golang.org/grpc"

	gomapb "go.chromium.org/goma/server/proto/api"
	pb "go.chromium.org/goma/server/proto/exec"
)

// TODO: expvar counter

// DefaultMaxReqMsgSize is max request message size for exec serivce.
// exec server may recieve (exec client may send) > 45MB
// if there are many embedded content.
// request from gce staging bot, 45MB is used.
// 64MB might not be sufficient enough.
// grpc's default is 4MB.
const DefaultMaxReqMsgSize = 64 * 1024 * 1024

// DefaultMaxRespMsgSize is max response size of exec service.
// exec server may send (exec client may receive) > 4MB.
// e.g. 2 outputs may close to 2MB (*.o, *.dwo) +
// other outputs (*.o.d, stdout, stderr).  http://b/79554706
// grpc's default is 4MB.
const DefaultMaxRespMsgSize = 6 * 1024 * 1024

// Client is a client to access exec service via gRPC.
type Client struct {
	addr     string
	dialOpts []grpc.DialOption
}

// NewClient creates new client to access exec service serving on address.
func NewClient(address string, opts ...grpc.DialOption) Client {
	return Client{
		addr:     address,
		dialOpts: opts,
	}
}

// Exec handles goma Exec requests.
func (c Client) Exec(ctx context.Context, in *gomapb.ExecReq, opts ...grpc.CallOption) (*gomapb.ExecResp, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/exec.Client.Exec")
	defer span.End()
	conn, err := grpc.DialContext(ctx, c.addr,
		append([]grpc.DialOption{
			grpc.WithBlock(),
		}, c.dialOpts...)...)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	resp, err := pb.NewExecServiceClient(conn).Exec(ctx, in,
		append([]grpc.CallOption{
			grpc.MaxCallSendMsgSize(DefaultMaxReqMsgSize),
			grpc.MaxCallRecvMsgSize(DefaultMaxRespMsgSize),
		}, opts...)...)

	return resp, err
}
