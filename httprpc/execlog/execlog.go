// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package execlog implements log service for goma httprpc.
package execlog

// TODO: go generate from proto?

import (
	"context"
	"net/http"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/httprpc"
	pb "go.chromium.org/goma/server/proto/api"
	execlogpb "go.chromium.org/goma/server/proto/execlog"
)

// Handler returns execlog service handler.
func Handler(s execlogpb.LogServiceServer, opts ...httprpc.HandlerOption) http.Handler {
	return httprpc.Handler(
		"LogService.SaveLog",
		&pb.SaveLogReq{}, &pb.SaveLogResp{},
		func(ctx context.Context, req proto.Message) (proto.Message, error) {
			resp, err := s.SaveLog(ctx, req.(*pb.SaveLogReq))
			return resp, err
		}, opts...)
}
