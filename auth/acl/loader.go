// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package acl

import (
	"context"
	"fmt"
	"io/ioutil"

	"github.com/golang/protobuf/proto"

	pb "go.chromium.org/goma/server/proto/auth"
)

// Loader loads acl data.
type Loader interface {
	Load(ctx context.Context) (*pb.ACL, error)
}

// StaticLoader loads static acl data.
type StaticLoader struct {
	*pb.ACL
}

func (l StaticLoader) Load(ctx context.Context) (*pb.ACL, error) {
	return l.ACL, nil
}

// FileLoader loads acl data from Filename.
type FileLoader struct {
	Filename string
}

// Loads loads acl stored as text proto in file.
func (l FileLoader) Load(ctx context.Context) (*pb.ACL, error) {
	b, err := ioutil.ReadFile(l.Filename)
	if err != nil {
		return nil, err
	}
	a := &pb.ACL{}
	err = proto.UnmarshalText(string(b), a)
	if err != nil {
		return nil, fmt.Errorf("load error %s: %v", l.Filename, err)
	}
	return a, nil
}
