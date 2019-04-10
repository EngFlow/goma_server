// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"

	"go.opencensus.io/trace"
	"google.golang.org/grpc"

	"go.chromium.org/goma/server/file"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	filepb "go.chromium.org/goma/server/proto/file"
	"go.chromium.org/goma/server/rpc"
)

// FileServer handles /s and /l.
type FileServer struct {
	Client filepb.FileServiceClient
}

// StoreFile handles /s.
func (s FileServer) StoreFile(ctx context.Context, req *gomapb.StoreFileReq) (*gomapb.StoreFileResp, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/backend.FileServer.StoreFile")
	defer span.End()
	ctx = passThroughContext(ctx)
	ctx, id := rpc.TagID(ctx, req.GetRequesterInfo())
	logger := log.FromContext(ctx)
	logger.Infof("call storefile %s", id)
	resp, err := s.Client.StoreFile(ctx, req, grpc.MaxCallSendMsgSize(file.DefaultMaxMsgSize))
	return resp, wrapError(ctx, "storefile", err)
}

// LookupFile handles /l.
func (s FileServer) LookupFile(ctx context.Context, req *gomapb.LookupFileReq) (*gomapb.LookupFileResp, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/backend.FileServer.StoreFile")
	defer span.End()
	ctx = passThroughContext(ctx)
	ctx, id := rpc.TagID(ctx, req.GetRequesterInfo())
	logger := log.FromContext(ctx)
	logger.Infof("call lookupfile %s", id)
	resp, err := s.Client.LookupFile(ctx, req, grpc.MaxCallRecvMsgSize(file.DefaultMaxMsgSize))
	return resp, wrapError(ctx, "lookupfile", err)
}
