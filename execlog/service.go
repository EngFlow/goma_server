// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package execlog

import (
	"context"

	gomapb "go.chromium.org/goma/server/proto/api"
)

// DefaultMaxReqMsgSize is max request message size for execlog service.
// execlog server may receives > 8MB.
// grpc's default is 4MB.
const DefaultMaxReqMsgSize = 10 * 1024 * 1024

// Service represents goma execlog service.
type Service struct {
}

// SaveLog discards execlog.
// TODO: implement saving logic to GCS?
func (s Service) SaveLog(ctx context.Context, req *gomapb.SaveLogReq) (*gomapb.SaveLogResp, error) {
	return &gomapb.SaveLogResp{}, nil
}
