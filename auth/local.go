// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package auth

import (
	"context"

	"google.golang.org/grpc"

	pb "go.chromium.org/goma/server/proto/auth"
)

type LocalClient struct {
	*Service
}

func (c LocalClient) Auth(ctx context.Context, in *pb.AuthReq, opts ...grpc.CallOption) (*pb.AuthResp, error) {
	resp, err := c.Service.Auth(ctx, in)
	return resp, err
}
