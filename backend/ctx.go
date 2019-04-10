// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"

	"go.chromium.org/goma/server/auth/enduser"
)

// passThroughContext passes through enuser credentials from incoming to outgoing.
func passThroughContext(ctx context.Context) context.Context {
	if user, ok := enduser.FromContext(ctx); ok {
		ctx = enduser.NewContext(ctx, user)
	}
	return ctx
}
