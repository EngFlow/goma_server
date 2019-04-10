// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"

	"go.opencensus.io/trace"
	"google.golang.org/grpc"

	"go.chromium.org/goma/server/execlog"
	gomapb "go.chromium.org/goma/server/proto/api"
	execlogpb "go.chromium.org/goma/server/proto/execlog"
)

// ExeclogServer handles /sl.
type ExeclogServer struct {
	Client execlogpb.LogServiceClient
}

// SaveLog handles /sl.
func (s ExeclogServer) SaveLog(ctx context.Context, req *gomapb.SaveLogReq) (*gomapb.SaveLogResp, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/backend.ExeclogServer.SaveLog")
	defer span.End()
	ctx = passThroughContext(ctx)
	resp, err := s.Client.SaveLog(ctx, req, grpc.MaxCallSendMsgSize(execlog.DefaultMaxReqMsgSize))
	return resp, wrapError(ctx, "execlog", err)
}
