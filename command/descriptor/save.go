// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package descriptor

import (
	"io/ioutil"
	"path/filepath"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/hash"

	pb "go.chromium.org/goma/server/proto/command"
)

// Save saves given cmd descriptor to a file in dir.
func Save(dir string, desc *pb.CmdDescriptor) error {
	b, err := proto.Marshal(desc)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filepath.Join(dir, "descriptors", hash.SHA256Content(b)), b, 0644)
	if err != nil {
		return err
	}
	return nil
}
