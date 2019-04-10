// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package exec implements exec service for goma httprpc.
package exec

// TODO: go generate from proto?

import (
	"context"
	"net/http"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/httprpc"
	pb "go.chromium.org/goma/server/proto/api"
	execpb "go.chromium.org/goma/server/proto/exec"
)

// Handler returns exec service handler.
func Handler(s execpb.ExecServiceServer, opts ...httprpc.HandlerOption) http.Handler {
	return httprpc.Handler(
		"ExecService.Exec",
		&pb.ExecReq{}, &pb.ExecResp{},
		func(ctx context.Context, req proto.Message) (proto.Message, error) {
			resp, err := s.Exec(ctx, req.(*pb.ExecReq))
			return resp, err
		}, opts...)
}
