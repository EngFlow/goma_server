// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
	"go.chromium.org/goma/server/command/descriptor/winpath"
	gomapb "go.chromium.org/goma/server/proto/api"
)

func TestGetPathsWithNoCommonDirPosix(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		paths []string
		want  []string
	}{
		{
			desc: "empty",
			// paths: nil,
			// want:  nil,
		},
		{
			desc:  "single",
			paths: []string{"/foo"},
			// want:  nil,
		},
		{
			desc: "has common dir",
			paths: []string{
				"/foo/bar1",
				"/foo/local/bar2",
				"/foo/bar3",
			},
			// want: nil,
		},
		{
			desc: "no common dir #0",
			paths: []string{
				"/foo",
				"/goo/local/baz",
				"/goo/",
				"/foo/local/bar2",
				"/foo/bar3",
			},
			want: []string{
				"/foo",
				"/goo/local/baz",
			},
		},
		{
			desc: "no common dir #1",
			paths: []string{
				"/foo/local/bar2",
				"/foo",
				"/goo/",
				"/goo/local/baz",
				"/foo/bar3",
			},
			want: []string{
				"/foo/local/bar2",
				"/goo/",
			},
		},
		{
			desc: "no common dir #2",
			paths: []string{
				"/goo",
				"/bar",
				"/baz/",
				"/foo",
			},
			want: []string{
				"/goo",
				"/bar",
			},
		},
	} {
		got := getPathsWithNoCommonDir(posixpath.FilePath{}, tc.paths)
		if !cmp.Equal(got, tc.want) {
			t.Errorf("test case %s: getPathsWithNoCommonDirPosix(paths=%v)=%v; want %v", tc.desc, tc.paths, got, tc.want)
		}
	}
}

func TestGetPathsWithNoCommonDirWin(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		paths []string
		want  []string
	}{
		{
			desc: `empty`,
			// paths: nil,
			// want:  nil,
		},
		{
			desc:  `single`,
			paths: []string{`/foo`},
			// want:  nil,
		},
		{
			desc: `has common dir`,
			paths: []string{
				`c:\foo\bar1`,
				`c:\foo\local\bar2`,
				`c:\foo\bar3`,
			},
			// want: nil,
		},
		{
			desc: `has common dir with differnt pathsep`,
			paths: []string{
				`c:/foo/bar1`,
				`c:\foo\local/bar2`,
				`c:\foo\bar3`,
			},
			// want: nil,
		},
		{
			desc: `has common dir with different case`,
			paths: []string{
				`C:\FOO\BAR1`,
				`C:\Foo\Local\Bar2`,
				`c:\foo\bar3`,
			},
			// want: nil,
		},
		{
			desc: `no common dir`,
			paths: []string{
				`c:\foo`,
				`c:\goo\local\baz`,
				`c:\goo\`,
				`c:\foo\local\bar2`,
				`c:\foo\bar3`,
			},
			want: []string{
				`c:\foo`,
				`c:\goo\local\baz`,
			},
		},
	} {
		got := getPathsWithNoCommonDir(winpath.FilePath{}, tc.paths)
		if !cmp.Equal(got, tc.want) {
			t.Errorf(`test case %s: getPathsWithNoCommonDirWin(paths=%v)=%v; want %v`, tc.desc, tc.paths, got, tc.want)
		}
	}
}

func TestInputRootDir(t *testing.T) {
	for _, tc := range []struct {
		desc        string
		req         *gomapb.ExecReq
		argv0       string
		allowChroot bool
		want        string
		wantChroot  bool
		wantPathErr bool
		wantRootErr bool
	}{
		{
			desc: "basic",
			req: &gomapb.ExecReq{
				Cwd: proto.String("/b/c/b/linux/src/out/Release"),
				Input: []*gomapb.ExecReq_Input{
					{
						Filename: proto.String("../../base/logging.h"),
					},
					{
						Filename: proto.String("../../build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					{
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
					{
						Filename: proto.String("/b/c/b/linux/src/out/Release/../../base/logging.h"),
					},
					{
						Filename: proto.String("/b/c/b/linux/src/out/Release/../../build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					{
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
					{
						Filename: proto.String("/b/c/b/linux/src/base/logging.h"),
					},
					{
						Filename: proto.String("/b/c/b/linux/src/build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					{
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
					{
						Filename: proto.String("../../base/logging.h"),
					},
					{
						Filename: proto.String("../../build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					{
						Filename: proto.String("gen/chrome/common/buildflags.h"),
					},
					{
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
					{
						Filename: proto.String("../../base/logging.h"),
					},
					{
						Filename: proto.String("../../build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					{
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
					{
						Filename: proto.String("gen/chrome/common/buildflags.h"),
					},
					{
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
					{
						Filename: proto.String("gen/chrome/common/buildflags.h"),
					},
					{
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
					{
						Filename: proto.String("../../base/logging.h"),
					},
					{
						Filename: proto.String("../../build/linux/debian_sid_amd64-sysroot/usr/include/stdio.h"),
					},
					{
						Filename: proto.String("gen/chrome/common/buildflags.h"),
					},
					{
						Filename: proto.String("/usr/lib/gcc/x86_64-linux-gnu/7/crtbegin.o"),
					},
				},
			},
			argv0:       "../../third_party/llvm-build/Release+Asserts/bin/clang++",
			wantRootErr: true,
		},
		{
			desc: "wantChroot for /usr",
			req: &gomapb.ExecReq{
				Cwd: proto.String("/home/foo/src/out/Release"),
				Input: []*gomapb.ExecReq_Input{
					{
						Filename: proto.String("/usr/include/config.h"),
					},
				},
			},
			argv0:       "../../third_party/llvm-build/Release+Asserts/bin/clang++",
			want:        "/",
			allowChroot: true,
			wantChroot:  true,
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
		got, needChroot, err := inputRootDir(posixpath.FilePath{}, paths, tc.allowChroot)
		if tc.wantRootErr {
			if err == nil {
				t.Errorf("inputRootDir(files)=%v, %t, nil; want err", got, needChroot)
			}
			continue
		}
		if err != nil || got != tc.want || needChroot != tc.wantChroot {
			t.Errorf("inputRootDir(files)=%v, %t, %v; want %v, %t, nil", got, needChroot, err, tc.want, tc.wantChroot)
		}
	}
}

func TestRootRelPosix(t *testing.T) {
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
		var filepath posixpath.FilePath
		got, err := rootRel(filepath, tc.fname, filepath.Clean(tc.cwd), filepath.Clean(tc.rootDir))
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

func TestRootRelWin(t *testing.T) {
	for _, tc := range []struct {
		fname, cwd, rootDir string
		want                string
		wantErr             bool
	}{
		{
			fname:   `..\..\base\foo.cc`,
			cwd:     `c:\Users\foo\src\chromium\src\out\Release`,
			rootDir: `c:\Users\foo\src\chromium\src`,
			want:    `out\Release\..\..\base\foo.cc`,
		},
		{ // should ignore case to get relative path but preserve original case.
			fname:   `..\..\Base\Foo.cc`,
			cwd:     `C:\Users\Foo\Src\Chromium\Src\Out\Release`,
			rootDir: `c:\users\foo\src\chromium\src`,
			want:    `Out\Release\..\..\Base\Foo.cc`,
		},
		{
			fname:   `c:\Users\foo\src\chromium\src\third_party\depot_tools\win_toolchain\vs_files\hash\win_sdk\bin\..\..\VC\Tols\MSVC\14.x.x\include\limit.h`,
			cwd:     `c:\Users\foo\src\chromium\src\out\Release`,
			rootDir: `c:\Users\foo\src\chromium\src`,
			want:    `third_party\depot_tools\win_toolchain\vs_files\hash\win_sdk\bin\..\..\VC\Tols\MSVC\14.x.x\include\limit.h`,
		},
		{
			fname:   `..\..\..\base\out-of-root.h`,
			cwd:     `c:\Users\foo\src\chromium\src\out\Release`,
			rootDir: `c:\Users\foo\src\chromium\src`,
			wantErr: true,
		},
	} {
		var filepath winpath.FilePath
		got, err := rootRel(filepath, tc.fname, filepath.Clean(tc.cwd), filepath.Clean(tc.rootDir))
		if tc.wantErr {
			if err == nil {
				t.Errorf("rootRel(winpath.FilePath, %q, %q, %q)=%v, nil; want error", tc.fname, tc.cwd, tc.rootDir, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("rootRel(winpath.FilePath, %q, %q, %q)=%q, %v; want %q, nil", tc.fname, tc.cwd, tc.rootDir, got, err, tc.want)
		}
	}
}

func BenchmarkRootRel(b *testing.B) {
	for i := 0; i < b.N; i++ {
		rootRel(posixpath.FilePath{}, "../../base/foo.cc", "/home/foo/src/chromium/src/out/Release", "/home/foo/src/chromium/src")
	}
}

func TestHasPrefixDir(t *testing.T) {
	for _, tc := range []struct {
		p, prefix string
		want      bool
	}{
		{
			p:      "/home/foo/bar",
			prefix: "/home/foo",
			want:   true,
		},
		{
			p:      "/home/foo",
			prefix: "/home/foo",
			want:   true,
		},
		{
			p:      "/home/foo/",
			prefix: "/home/foo",
			want:   true,
		},
		// hasPrefixDir trim "/" in `prefix`
		{
			p:      "/home/foo",
			prefix: "/home/foo////////////////",
			want:   true,
		},
		{
			p:      "/foo",
			prefix: "/bar",
			want:   false,
		},
		{
			p:      "/foo/bar",
			prefix: "/bar",
			want:   false,
		},
		{
			p:      "/foo",
			prefix: "/bar/baz",
			want:   false,
		},
		{
			p:      "/foo",
			prefix: "/foo/bar",
			want:   false,
		},
		{
			p:      "/home/foobar",
			prefix: "/home/foo",
			want:   false,
		},

		{
			p:      "home/foo",
			prefix: "home/foo",
			want:   true,
		},
		{
			p:      "home/foo/bar",
			prefix: "home/foo",
			want:   true,
		},
	} {
		got := hasPrefixDir(tc.p, tc.prefix)
		if got != tc.want {
			t.Errorf("hasPrefixDir(%s,%s) = %t; want %t", tc.p, tc.prefix, got, tc.want)
		}
	}
}
