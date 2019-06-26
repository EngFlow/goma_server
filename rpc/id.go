// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package rpc

import (
	"context"
	"fmt"

	"go.opencensus.io/tag"

	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
)

var (
	requestID = tag.MustNewKey("go.chromium.org/goma/server/rpc.request_id")
)

func init() {
	log.RegisterTagKey(requestID)
}

// RequestID returns string identifier of this request.
func RequestID(r *gomapb.RequesterInfo) string {
	return fmt.Sprintf("%s#%d", r.GetCompilerProxyId(), r.GetRetry())
}

// TagID tags request id in context.
func TagID(ctx context.Context, r *gomapb.RequesterInfo) (context.Context, string) {
	logger := log.FromContext(ctx)
	id := RequestID(r)
	tctx, err := tag.New(ctx, tag.Upsert(requestID, id))
	if err != nil {
		logger.Errorf("tag error: %v", err)
		return ctx, id
	}
	return tctx, id
}
