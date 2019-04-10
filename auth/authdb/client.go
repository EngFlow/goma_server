// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package authdb

import (
	"context"

	"go.chromium.org/goma/server/httprpc"
	"go.chromium.org/goma/server/log"
	pb "go.chromium.org/goma/server/proto/auth"
	"go.chromium.org/goma/server/rpc"
)

// Client is authdb client.
type Client struct {
	*httprpc.Client
}

// IsMember checks email is in group.
func (c Client) IsMember(ctx context.Context, email, group string) bool {
	logger := log.FromContext(ctx)

	req := &pb.CheckMembershipReq{
		Email: email,
		Group: group,
	}
	resp := &pb.CheckMembershipResp{}
	err := rpc.Retry{}.Do(ctx, func() error {
		return c.Client.Call(ctx, req, resp)
	})
	if err != nil {
		logger.Errorf("check membership: %v", err)
		return false
	}
	return resp.IsMember
}
