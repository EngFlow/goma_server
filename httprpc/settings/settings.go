// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package settings implements settings service for goma httprpc.
package settings

import (
	"context"
	"net/http"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/httprpc"
	pb "go.chromium.org/goma/server/proto/settings"
)

func Handler(s pb.SettingsServiceServer, opts ...httprpc.HandlerOption) http.Handler {
	return httprpc.Handler(
		"SettingsService.Get",
		&pb.SettingsReq{}, &pb.SettingsResp{},
		func(ctx context.Context, req proto.Message) (proto.Message, error) {
			resp, err := s.Get(ctx, req.(*pb.SettingsReq))
			return resp, err
		}, opts...)
}

func Register(mux *http.ServeMux, s pb.SettingsServiceServer, opts ...httprpc.HandlerOption) {
	mux.Handle("/settings", Handler(s, opts...))
}
