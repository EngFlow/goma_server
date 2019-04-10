// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package descriptor

import (
	"io/ioutil"
	"path/filepath"

	"github.com/golang/protobuf/proto"

	pb "go.chromium.org/goma/server/proto/command"
)

func Load(dir string) ([]*pb.CmdDescriptor, error) {
	names, err := filepath.Glob(filepath.Join(dir, "descriptors", "*"))
	if err != nil {
		return nil, err
	}
	var descs []*pb.CmdDescriptor
	for _, name := range names {
		b, err := ioutil.ReadFile(name)
		if err != nil {
			return nil, err
		}
		d := &pb.CmdDescriptor{}
		err = proto.Unmarshal(b, d)
		if err != nil {
			return nil, err
		}
		descs = append(descs, d)
	}
	return descs, nil
}
