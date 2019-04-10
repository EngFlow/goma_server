// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package main

import (
	"testing"

	"go.chromium.org/goma/server/exec"
	"go.chromium.org/goma/server/execlog"
	"go.chromium.org/goma/server/file"
)

func TestMaxMsgSize(t *testing.T) {
	if maxMsgSize < file.DefaultMaxMsgSize {
		t.Errorf("%d < %d (file)", maxMsgSize, file.DefaultMaxMsgSize)
	}
	if maxMsgSize < exec.DefaultMaxReqMsgSize {
		t.Errorf("%d < %d (exec)", maxMsgSize, exec.DefaultMaxReqMsgSize)
	}
	if maxMsgSize < execlog.DefaultMaxReqMsgSize {
		t.Errorf("%d < %d (execlog)", maxMsgSize, execlog.DefaultMaxReqMsgSize)
	}
}
