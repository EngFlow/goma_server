// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"testing"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
	gomapb "go.chromium.org/goma/server/proto/api"
)

func TestInputRootDir(t *testing.T) {
	for _, tc := range []struct {
		desc        string
		req         *gomapb.ExecReq
		argv0       string
		want        string
		wantPathErr bool
		wantRootErr bool
	}{
		{
			desc: "basic",
			req: &gomapb.ExecReq{
				Cwd: proto.String("/b/c/b/linux/src/out/Release"),
				Input: []*gomapb.ExecReq_Input{
					&gomapb.ExecReq_Input{
						Filename: proto.String("../../base/logging.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("../../build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("gen/chrome/common/buildflags.h"),
					},
				},
			},
			argv0: "../../third_party/llvm-build/Release+Asserts/bin/clang++",
			want:  "/b/c/b/linux/src",
		},
		{
			desc: "abs input path",
			req: &gomapb.ExecReq{
				Cwd: proto.String("/b/c/b/linux/src/out/Release"),
				Input: []*gomapb.ExecReq_Input{
					&gomapb.ExecReq_Input{
						Filename: proto.String("/b/c/b/linux/src/out/Release/../../base/logging.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("/b/c/b/linux/src/out/Release/../../build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("/b/c/b/linux/src/out/Release/gen/chrome/common/buildflags.h"),
					},
				},
			},
			argv0: "../../third_party/llvm-build/Release+Asserts/bin/clang++",
			want:  "/b/c/b/linux/src",
		},
		{
			desc: "abs resolved input path",
			req: &gomapb.ExecReq{
				Cwd: proto.String("/b/c/b/linux/src/out/Release"),
				Input: []*gomapb.ExecReq_Input{
					&gomapb.ExecReq_Input{
						Filename: proto.String("/b/c/b/linux/src/base/logging.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("/b/c/b/linux/src/build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("/b/c/b/linux/src/out/Release/gen/chrome/common/buildflags.h"),
					},
				},
			},
			argv0: "../../third_party/llvm-build/Release+Asserts/bin/clang++",
			want:  "/b/c/b/linux/src",
		},
		{
			desc: "no common path",
			req: &gomapb.ExecReq{
				Cwd: proto.String("/b/c/b/linux/src/out/Release"),
				Input: []*gomapb.ExecReq_Input{
					&gomapb.ExecReq_Input{
						Filename: proto.String("../../base/logging.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("../../build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("gen/chrome/common/buildflags.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("/usr/local/include/foo.h"),
					},
				},
			},
			argv0:       "../../third_party/llvm-build/Release+Asserts/bin/clang++",
			wantRootErr: true,
		},
		{
			desc: "non-abs cwd",
			req: &gomapb.ExecReq{
				Cwd: proto.String("b/c/b/linux/src/out/Release"),
				Input: []*gomapb.ExecReq_Input{
					&gomapb.ExecReq_Input{
						Filename: proto.String("../../base/logging.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("../../build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("gen/chrome/common/buildflags.h"),
					},
				},
			},
			argv0:       "../../third_party/llvm-build/Release+Asserts/bin/clang++",
			wantPathErr: true,
		},
		{
			desc: "gen files only",
			req: &gomapb.ExecReq{
				Cwd: proto.String("/b/c/b/linux/src/out/Release"),
				Input: []*gomapb.ExecReq_Input{
					&gomapb.ExecReq_Input{
						Filename: proto.String("gen/chrome/common/buildflags.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("gen/chrome/common/buildflags.cc"),
					},
				},
			},
			argv0: "../../third_party/llvm-build/Release+Asserts/bin/clang++",
			want:  "/b/c/b/linux/src",
		},
		{
			desc: "ignore /usr/bin/gcc",
			req: &gomapb.ExecReq{
				Cwd: proto.String("/b/c/b/linux/src/out/Release"),
				Input: []*gomapb.ExecReq_Input{
					&gomapb.ExecReq_Input{
						Filename: proto.String("gen/chrome/common/buildflags.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("gen/chrome/common/buildflags.cc"),
					},
				},
			},
			argv0: "/usr/bin/gcc",
			want:  "/b/c/b/linux/src/out/Release",
		},
		{
			desc: "cover system dirs (/usr)",
			req: &gomapb.ExecReq{
				Cwd: proto.String("/usr/home/foo/src/out/Release"),
				Input: []*gomapb.ExecReq_Input{
					&gomapb.ExecReq_Input{
						Filename: proto.String("../../base/logging.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("../../build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("gen/chrome/common/buildflags.h"),
					},
					&gomapb.ExecReq_Input{
						Filename: proto.String("/usr/lib/gcc/x86_64-linux-gnu/7/crtbegin.o"),
					},
				},
			},
			argv0:       "../../third_party/llvm-build/Release+Asserts/bin/clang++",
			wantRootErr: true,
		},
	} {
		t.Logf("test case: %s", tc.desc)
		paths, err := inputPaths(posixpath.FilePath{}, tc.req, tc.argv0)
		if tc.wantPathErr {
			if err == nil {
				t.Errorf("inputPaths(req, %q)=%v", tc.argv0, paths)
			}
			continue
		}
		if err != nil {
			t.Errorf("inputPaths(req, %q)=%v, %v; want nil error", tc.argv0, paths, err)
		}
		got, err := inputRootDir(posixpath.FilePath{}, paths)
		if tc.wantRootErr {
			if err == nil {
				t.Errorf("inputRootDir(files)=%v, nil; want err", got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("inputRootDir(files)=%v, %v; want %v, nil", got, err, tc.want)
		}
	}
}

func TestRootRel(t *testing.T) {
	for _, tc := range []struct {
		fname, cwd, rootDir string
		want                string
		wantErr             bool
	}{
		{
			fname:   "../../base/foo.cc",
			cwd:     "/home/foo/src/chromium/src/out/Release",
			rootDir: "/home/foo/src/chromium/src",
			want:    "out/Release/../../base/foo.cc",
		},
		{
			fname:   "gen/foo.cc",
			cwd:     "/home/foo/src/chromium/src/out/Release",
			rootDir: "/home/foo/src/chromium/src",
			want:    "out/Release/gen/foo.cc",
		},
		{
			fname:   "/home/foo/src/chromium/src/third_party/depot_tools/win_toolchain/vs_files/hash/win_sdk/bin/../../VC/Tols/MSVC/14.x.x/include/limit.h",
			cwd:     "/home/foo/src/chromium/src/out/Release",
			rootDir: "/home/foo/src/chromium/src",
			want:    "third_party/depot_tools/win_toolchain/vs_files/hash/win_sdk/bin/../../VC/Tols/MSVC/14.x.x/include/limit.h",
		},
		{
			fname:   "../../../base/out-of-root.h",
			cwd:     "/home/foo/src/chromium/src/out/Release",
			rootDir: "/home/foo/src/chromium/src",
			wantErr: true,
		},
		{
			fname:   "../../base/../../other/out-of-root.h",
			cwd:     "/home/foo/src/chromium/src/out/Release",
			rootDir: "/home/foo/src/chromium/src",
			wantErr: true,
		},
		{
			fname:   "/usr/bin/objcopy",
			cwd:     "/home/foo/src/chromium/src/out/Release",
			rootDir: "/home/foo/src/chromium/src",
			wantErr: true,
		},
		{
			fname:   "../../base/foo.h",
			cwd:     "/home/bar/../foo/src/chromium/src/out/Release",
			rootDir: "/home/foo/src/chromium/src",
			want:    "out/Release/../../base/foo.h",
		},
		{
			fname:   "../../base/../a/../b/foo.h",
			cwd:     "/home/bar/../foo/src/chromium/src/out/Release",
			rootDir: "/home/foo/src/chromium/src",
			want:    "out/Release/../../base/../a/../b/foo.h",
		},
		{
			fname:   "../../base/../../a/b/../c/foo.h",
			cwd:     "/home/bar/../foo/src/chromium/src/out/Release",
			rootDir: "/home/foo/src/chromium/src",
			wantErr: true,
		},
	} {
		got, err := rootRel(posixpath.FilePath{}, tc.fname, tc.cwd, tc.rootDir)
		if tc.wantErr {
			if err == nil {
				t.Errorf("rootRel(posixpath.FilePath, %q, %q, %q)=%v, nil; want error", tc.fname, tc.cwd, tc.rootDir, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("rootRel(posixpath.FilePath, %q, %q, %q)=%q, %v; want %q, nil", tc.fname, tc.cwd, tc.rootDir, got, err, tc.want)
		}
	}
}
