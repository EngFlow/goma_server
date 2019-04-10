// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"

	"go.opencensus.io/trace"
	"google.golang.org/grpc"

	"go.chromium.org/goma/server/exec"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	execpb "go.chromium.org/goma/server/proto/exec"
	"go.chromium.org/goma/server/rpc"
)

// ExecServer handles /e.
type ExecServer struct {
	Client execpb.ExecServiceClient
}

// Exec handles /e.
func (s ExecServer) Exec(ctx context.Context, req *gomapb.ExecReq) (*gomapb.ExecResp, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/backend.ExecServer.Exec")
	defer span.End()
	ctx = passThroughContext(ctx)
	ctx, id := rpc.TagID(ctx, req.GetRequesterInfo())
	logger := log.FromContext(ctx)
	logger.Infof("call exec %s", id)
	resp, err := s.Client.Exec(ctx, req, grpc.MaxCallSendMsgSize(exec.DefaultMaxReqMsgSize), grpc.MaxCallRecvMsgSize(exec.DefaultMaxRespMsgSize))
	return resp, wrapError(ctx, "exec", err)
}
