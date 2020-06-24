// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.


package remoteexec

import (
	"context"
	"errors"

	gomapb "go.chromium.org/goma/server/proto/api"
	"go.chromium.org/goma/server/remoteexec/digest"
)

// adjustExecReq adjust req if needed.
func adjustExecReq(req *gomapb.ExecReq) {
}

// wrapperForWindows. not yet supported.
func wrapperForWindows(ctx context.Context) (string, digest.Data, error) {
	return "", nil, errors.New("not yet supported")
}
