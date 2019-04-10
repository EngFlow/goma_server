// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package file

import (
	"go.chromium.org/goma/server/hash"
	gomapb "go.chromium.org/goma/server/proto/api"
)

// Key returns hash key for blob.
func Key(blob *gomapb.FileBlob) (string, error) {
	return hash.SHA256Proto(blob)
}
