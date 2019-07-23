// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"testing"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
	gomapb "go.chromium.org/goma/server/proto/api"
)

func TestPathFromToolchainSpec(t *testing.T) {
	for _, tc := range []struct {
		desc string
		cfp  clientFilePath
		ts   []*gomapb.ToolchainSpec
		want string
	}{
		{
			desc: "empty",
			cfp:  posixpath.FilePath{},
			ts:   nil,
			want: "",
		},
		{
			desc: "one toolchain spec",
			cfp:  posixpath.FilePath{},
			ts: []*gomapb.ToolchainSpec{
				{
					Path:         proto.String("/usr/bin/clang"),
					Hash:         proto.String("fe4c1bb3b68376901c9f9e87dc1196a81f598eb854061ddfc5f85ef7e054feed"),
					Size:         proto.Int64(86003704),
					IsExecutable: proto.Bool(true),
				},
			},
			want: "/usr/bin",
		},
		{
			desc: "toolchain spec in the same directory",
			cfp:  posixpath.FilePath{},
			ts: []*gomapb.ToolchainSpec{
				{
					Path:         proto.String("/usr/bin/clang"),
					Hash:         proto.String("fe4c1bb3b68376901c9f9e87dc1196a81f598eb854061ddfc5f85ef7e054feed"),
					Size:         proto.Int64(86003704),
					IsExecutable: proto.Bool(true),
				},
				{
					Path:        proto.String("/usr/bin/clang++"),
					SymlinkPath: proto.String("clang"),
				},
			},
			want: "/usr/bin",
		},
		{
			desc: "toolchain spec in the different directory",
			cfp:  posixpath.FilePath{},
			ts: []*gomapb.ToolchainSpec{
				{
					Path:         proto.String("/usr/bin/clang"),
					Hash:         proto.String("fe4c1bb3b68376901c9f9e87dc1196a81f598eb854061ddfc5f85ef7e054feed"),
					Size:         proto.Int64(86003704),
					IsExecutable: proto.Bool(true),
				},
				{
					Path:         proto.String("/bin/bash"),
					Hash:         proto.String("d80e7ffe8836eb30bafb138def4c1a6e3f586d98ec39a26d9549a92141e95281"),
					Size:         proto.Int64(715328),
					IsExecutable: proto.Bool(true),
				},
			},
			want: "/bin:/usr/bin",
		},
		{
			desc: "complexed",
			cfp:  posixpath.FilePath{},
			ts: []*gomapb.ToolchainSpec{
				{
					Path:         proto.String("../../native_client/toolchain/linux_x86/pnacl_newlib/bin/pnacl-clang++"),
					Hash:         proto.String("dummy"),
					Size:         proto.Int64(0),
					IsExecutable: proto.Bool(true),
				},
				{
					Path:         proto.String("/home/goma/chrome_root/src/native_client/toolchain/linux_x86/pnacl_newlib/bin/clang"),
					Hash:         proto.String("dummy"),
					Size:         proto.Int64(0),
					IsExecutable: proto.Bool(true),
				},
				{
					// lib directory is also included.
					Path: proto.String("/usr/lib64/python2.7/abc.py"),
					Hash: proto.String("dummy"),
					Size: proto.Int64(0),
				},
				{
					Path:        proto.String("/usr/bin/python"),
					SymlinkPath: proto.String("python-wrapper"),
				},
				{
					Path:         proto.String("/usr/bin/python-wrapper"),
					Hash:         proto.String("dummy"),
					Size:         proto.Int64(0),
					IsExecutable: proto.Bool(true),
				},
				{
					Path:         proto.String("/usr/bin/bash"),
					Hash:         proto.String("dummy"),
					Size:         proto.Int64(0),
					IsExecutable: proto.Bool(true),
				},
				{
					Path:         proto.String("/lib64/libc-2.27.so"),
					Hash:         proto.String("dummy"),
					Size:         proto.Int64(0),
					IsExecutable: proto.Bool(true),
				},
			},
			want: "../../native_client/toolchain/linux_x86/pnacl_newlib/bin:/home/goma/chrome_root/src/native_client/toolchain/linux_x86/pnacl_newlib/bin:/lib64:/usr/bin:/usr/lib64/python2.7",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			actual := pathFromToolchainSpec(tc.cfp, tc.ts)
			if actual != tc.want {
				t.Errorf("pathFromToolchainSpec(%v, %v) = %q; want %q", tc.cfp, tc.ts, actual, tc.want)
			}
		})
	}
}
