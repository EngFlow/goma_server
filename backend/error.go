// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"

	netctx "golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/log"
)

func wrapError(ctx context.Context, service string, err error) error {
	logger := log.FromContext(ctx)
	if err == nil {
		logger.Debugf("call %s done", service)
		return nil
	}
	st, _ := status.FromError(err)
	code := st.Code()
	if code == codes.Unknown {
		switch ctx.Err() {
		case context.Canceled, netctx.Canceled:
			code = codes.Canceled
		case context.DeadlineExceeded, netctx.DeadlineExceeded:
			code = codes.DeadlineExceeded
		}
	}
	err = grpc.Errorf(code, "failed to call %s: %s", service, st.Message())
	switch code {
	case codes.Unavailable, codes.Canceled, codes.Aborted:
		logger.Warnf("call %s err: %v", service, err)
	default:
		logger.Errorf("call %s err: %v", service, err)
	}
	return err
}
