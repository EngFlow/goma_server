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
	requestID = mustTagNewKey("go.chromium.org/goma/server/rpc.request_id")
)

func mustTagNewKey(name string) tag.Key {
	k, err := tag.NewKey(name)
	if err != nil {
		logger := log.FromContext(context.Background())
		logger.Fatal(err)
	}
	log.RegisterTagKey(k)
	return k
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
