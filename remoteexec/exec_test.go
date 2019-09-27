// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"testing"

	"github.com/golang/protobuf/proto"

	gomapb "go.chromium.org/goma/server/proto/api"
	"go.chromium.org/goma/server/remoteexec/merkletree"
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

func TestChangeSymlinkAbsToRel(t *testing.T) {
	for _, tc := range []struct {
		desc       string
		name       string
		target     string
		wantTarget string
		wantErr    bool
	}{
		{
			desc:       "base",
			name:       "/a/b.txt",
			target:     "/c/d.txt",
			wantTarget: "../c/d.txt",
		},
		{
			desc:    "should be error because name is not absolute",
			name:    "../a/b.txt",
			target:  "/c/d.txt",
			wantErr: true,
		},
		{
			desc:       "should allow name is in root",
			name:       "/a.txt",
			target:     "/c/d.txt",
			wantTarget: "c/d.txt",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			e := merkletree.Entry{
				Name:   tc.name,
				Target: tc.target,
			}
			actual, err := changeSymlinkAbsToRel(e)
			if tc.wantErr && err == nil {
				t.Errorf("changeSymlinkAbsToRel(%v) returned nil err; want err", e)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("changeSymlinkAbsToRel(%v) returned %v ; want nil", e, err)
			}
			if tc.wantTarget != actual.Target {
				expected := merkletree.Entry{
					Name:   e.Name,
					Target: tc.wantTarget,
				}
				t.Errorf("changeSymlinkAbsToRel(%v) = %v; want %v", e, actual, expected)
			}
		})
	}
}
