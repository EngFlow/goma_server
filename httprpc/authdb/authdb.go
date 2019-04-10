// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package authdb implements authdb service for goma httprpc.
package authdb

import (
	"context"
	"net/http"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/httprpc"
	pb "go.chromium.org/goma/server/proto/auth"
)

func Handler(s pb.AuthDBServiceServer, opts ...httprpc.HandlerOption) http.Handler {
	return httprpc.Handler(
		"AuthDBSerrvice.CheckMembership",
		&pb.CheckMembershipReq{}, &pb.CheckMembershipResp{},
		func(ctx context.Context, req proto.Message) (proto.Message, error) {
			resp, err := s.CheckMembership(ctx, req.(*pb.CheckMembershipReq))
			return resp, err
		}, opts...)
}

func Register(mux *http.ServeMux, s pb.AuthDBServiceServer, opts ...httprpc.HandlerOption) {
	mux.Handle("/authdb/checkMembership", Handler(s, opts...))
}
