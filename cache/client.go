// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package cache

import (
	"context"

	"google.golang.org/grpc"

	"go.chromium.org/goma/server/rpc"

	pb "go.chromium.org/goma/server/proto/cache"
)

// TODO: opencensus metrics.

// Client is a client to access cache service via gRPC.
type Client struct {
	client *rpc.Client
}

// NewClient creates new client to access cache service serving on address.
// cache service will be sharded by key.
func NewClient(ctx context.Context, address string, opts ...grpc.DialOption) Client {
	return Client{
		client: rpc.NewClient(ctx, address,
			func(cc *grpc.ClientConn) interface{} {
				return pb.NewCacheServiceClient(cc)
			},
			rpc.DialOptions(opts...)...),
	}
}

// Close releases the resources used by the client.
func (c Client) Close() error {
	return c.client.Close()
}

// Get gets key-value data for requested key.
func (c Client) Get(ctx context.Context, in *pb.GetReq, opts ...grpc.CallOption) (*pb.GetResp, error) {
	var resp *pb.GetResp
	var err error
	err = c.client.Call(ctx, c.client.Shard, in.Key,
		func(client interface{}) error {
			resp, err = client.(pb.CacheServiceClient).Get(ctx, in, opts...)
			return err
		})
	return resp, err
}

// Put puts new key-value data.
// If no key-value is given, do nothing.
func (c Client) Put(ctx context.Context, in *pb.PutReq, opts ...grpc.CallOption) (*pb.PutResp, error) {
	if in.Kv == nil {
		return nil, nil
	}
	var resp *pb.PutResp
	var err error
	err = c.client.Call(ctx, c.client.Shard, in.Kv.Key,
		func(client interface{}) error {
			resp, err = client.(pb.CacheServiceClient).Put(ctx, in, opts...)
			return err
		})
	return resp, err
}
