// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package file

import (
	gomapb "go.chromium.org/goma/server/proto/api"
)

// IsValid checks blob is valid FileBlob.
func IsValid(blob *gomapb.FileBlob) bool {
	if blob == nil {
		return false
	}
	if blob.GetBlobType() == gomapb.FileBlob_FILE_UNSPECIFIED {
		return false
	}
	// TODO: add check more
	return true
}
