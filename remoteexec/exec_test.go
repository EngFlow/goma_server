// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"testing"

	"github.com/golang/protobuf/proto"

	gomapb "go.chromium.org/goma/server/proto/api"
)

func TestSortMissing(t *testing.T) {
	inputs := []*gomapb.ExecReq_Input{
		{
			Filename: proto.String("../src/hello.cc"),
			HashKey:  proto.String("hash-hello.cc"),
		},
		{
			Filename: proto.String("../include/base.h"),
			HashKey:  proto.String("hash-base.h"),
		},
		{
			Filename: proto.String("../include/hello.h"),
			HashKey:  proto.String("hash-hello.h"),
		},
	}

	resp := &gomapb.ExecResp{
		MissingInput: []string{
			"../include/hello.h",
			"../src/hello.cc",
		},
		MissingReason: []string{
			"missing-hello.h",
			"missing-hello.cc",
		},
	}

	sortMissing(inputs, resp)
	want := &gomapb.ExecResp{
		MissingInput: []string{
			"../src/hello.cc",
			"../include/hello.h",
		},
		MissingReason: []string{
			"missing-hello.cc",
			"missing-hello.h",
		},
	}
	if !proto.Equal(resp, want) {
		t.Errorf("sortMissing: %s != %s", resp, want)
	}

	resp = proto.Clone(want).(*gomapb.ExecResp)
	sortMissing(inputs, resp)
	if !proto.Equal(resp, want) {
		t.Errorf("sortMissing (stable): %s != %s", resp, want)
	}
}
