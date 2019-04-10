// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package cache

import (
	"context"

	"google.golang.org/grpc"

	pb "go.chromium.org/goma/server/proto/cache"
)

// LocalClient is an adaptor to make CacheServiceServer as CacheServiceClient,
// ignoring any grpc.CallOption.
// TODO: use localhost server? https://github.com/grpc/grpc-go/issues/520
type LocalClient struct {
	pb.CacheServiceServer
}

func (c LocalClient) Get(ctx context.Context, in *pb.GetReq, opts ...grpc.CallOption) (*pb.GetResp, error) {
	return c.CacheServiceServer.Get(ctx, in)
}

func (c LocalClient) Put(ctx context.Context, in *pb.PutReq, opts ...grpc.CallOption) (*pb.PutResp, error) {
	return c.CacheServiceServer.Put(ctx, in)
}
