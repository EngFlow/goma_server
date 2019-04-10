// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Package rpc provides goma specific rpc features on gRPC.

Load balancing RPC client

gRPC doesn't provide good loadbalancing client (for kubernetes, yet).
Typical gRPC client could be created as follows:

	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

This package provides load balancing client if address has multiple IP
addresses:

	type GreeterClient struct {
		c *rpc.Client
	}

	func NewGreeterClient(address string, opts ...grpc.DialOption) GreeterClient {
		return GreeterClient{
			c: rpc.NewClient(address,
				func(cc *grpc.ClientConn) interface{} {
					return pb.NewGreeterClient(cc)
				}, opts...),
		}
	}

	func (c GreeterClient) SayHello(ctx context.Context, in *pb.Req, opts...grpc.CallOption) (*pb.Resp, error) {
		var resp *pb.Resp
		var err error
		err = c.Call(ctx, c.client.Pick, "",
			func(client interface{}) error {
				resp, err = client.(GreeterClient).SayHello(ctx, in, opts...)
				return err
			},
		)
		return resp, err
	}

	c := NewGreeterClient(address, grpc.WithInsecure())

rpc call would return codes.Unavailable if no backends available for address.

TODO: use grpc's Balancer?
TODO: use statefulset for sharding?
*/
package rpc
