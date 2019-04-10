// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package authdb

import (
	"context"

	pb "go.chromium.org/goma/server/proto/auth"
)

// AuthDB is authentication database.
type AuthDB interface {
	IsMember(ctx context.Context, email, group string) bool
}

// Handler handles request to AuthDB.
type Handler struct {
	AuthDB AuthDB
}

func (h Handler) CheckMembership(ctx context.Context, req *pb.CheckMembershipReq) (*pb.CheckMembershipResp, error) {
	return &pb.CheckMembershipResp{
		IsMember: h.AuthDB.IsMember(ctx, req.Email, req.Group),
	}, nil
}
