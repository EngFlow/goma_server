// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package file implements file service for goma httprpc.
package file

// TODO: go generate from proto?

import (
	"context"
	"net/http"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/httprpc"
	pb "go.chromium.org/goma/server/proto/api"
	filepb "go.chromium.org/goma/server/proto/file"
)

// StoreHandler returns file service StoreFile handler.
func StoreHandler(s filepb.FileServiceServer, opts ...httprpc.HandlerOption) http.Handler {
	return httprpc.Handler(
		"FileService.StoreFile",
		&pb.StoreFileReq{}, &pb.StoreFileResp{},
		func(ctx context.Context, req proto.Message) (proto.Message, error) {
			resp, err := s.StoreFile(ctx, req.(*pb.StoreFileReq))
			return resp, err
		}, opts...)
}

// LookupHandler returns file service LookupFile handler.
func LookupHandler(s filepb.FileServiceServer, opts ...httprpc.HandlerOption) http.Handler {
	return httprpc.Handler(
		"FileService.LookupFile",
		&pb.LookupFileReq{}, &pb.LookupFileResp{},
		func(ctx context.Context, req proto.Message) (proto.Message, error) {
			resp, err := s.LookupFile(ctx, req.(*pb.LookupFileReq))
			return resp, err
		}, opts...)
}
