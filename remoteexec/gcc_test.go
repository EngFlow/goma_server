// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"reflect"
	"testing"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
)

func TestGccCwdAgnostic(t *testing.T) {
	for _, tc := range []struct {
		desc        string
		args        []string
		envs        []string
		cwdAgnostic bool
	}{
		{
			desc: "chromium base release",
			args: []string{
				"../../third_party/llvm-build/Release+Asserts/bin/clang++",
				"-MMD",
				"-MF",
				"obj/base/base/time.o.d",
				"-DUSE_SYMBOLIZE",
				"-I../..",
				"-fno-strict-aliasing",
				"--param=ssp-buffer-size=4",
				"-fPIC",
				"-pipe",
				"-B../../third_party/binutils/Linux_x64/Release/bin",
				"-pthread",
				"-Xclang",
				"-mllvm",
				"-Xclang",
				"-instcombine-lower-dbg-declare=0",
				"-no-canonical-prefixes",
				"-m64",
				"-march=x86-64",
				"-Wall",
				"-g0",
				"-Xclang",
				"-load",
				"-Xclang",
				"../../third_party/llvm-build/Release+Asserts/lib/libFindBadCustructs.so",
				"-Xclang",
				"-add-plugin",
				"-Xclang",
				"find-bad-constructs",
				"-Xclang",
				"-plugin-arg-find-bad-constructs",
				"-Xclang",
				"check-enum-max-value",
				"-isystem../../build/linux/debian_sid_amd64-sysroot/usr/include/glib-2.0",
				"-O2",
				"--sysroot=../../build/linux/debian_sid_amd64-sysroot",
				"-c",
				"../../base/time/time.cc",
				"-o",
				"obj/base/base/time.o",
			},
			envs: []string{
				"PWD=/b/c/b/linux/src/out/Release",
			},
			cwdAgnostic: true,
		},
		{
			desc: "chromium base debug",
			args: []string{
				"../../third_party/llvm-build/Release+Asserts/bin/clang++",
				"-MMD",
				"-MF",
				"obj/base/base/time.o.d",
				"-DUSE_SYMBOLIZE",
				"-I../..",
				"-fno-strict-aliasing",
				"--param=ssp-buffer-size=4",
				"-fPIC",
				"-pipe",
				"-B../../third_party/binutils/Linux_x64/Release/bin",
				"-pthread",
				"-Xclang",
				"-mllvm",
				"-Xclang",
				"-instcombine-lower-dbg-declare=0",
				"-no-canonical-prefixes",
				"-m64",
				"-march=x86-64",
				"-Wall",
				"-g2",
				"-gsplit-dwarf",
				"-Xclang",
				"-load",
				"-Xclang",
				"../../third_party/llvm-build/Release+Asserts/lib/libFindBadCustructs.so",
				"-Xclang",
				"-add-plugin",
				"-Xclang",
				"find-bad-constructs",
				"-Xclang",
				"-plugin-arg-find-bad-constructs",
				"-Xclang",
				"check-enum-max-value",
				"-isystem../../build/linux/debian_sid_amd64-sysroot/usr/include/glib-2.0",
				"-O2",
				"--sysroot=../../build/linux/debian_sid_amd64-sysroot",
				"-c",
				"../../base/time/time.cc",
				"-o",
				"obj/base/base/time.o",
			},
			envs: []string{
				"PWD=/b/c/b/linux/src/out/Debug",
			},
			cwdAgnostic: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			err := gccCwdAgnostic(posixpath.FilePath{}, tc.args, tc.envs)
			if (err == nil) != tc.cwdAgnostic {
				t.Errorf("gccCwdAgnostic(posixpath.FilePath, args, envs)=%v; cwdAgnostic=%t", err, tc.cwdAgnostic)
			}
		})
	}
}

func TestGccOutputs(t *testing.T) {
	for _, tc := range []struct {
		desc string
		args []string
		want []string
	}{
		{
			desc: "basic",
			args: []string{
				"gcc", "-c", "A/test.c",
				"-I", "A/B/C",
				"-ID/E/F",
				"-o", "A/test.o",
				"-MF", "test.d",
			},
			want: []string{"test.d", "A/test.o"},
		},
		{
			desc: "prefix",
			args: []string{
				"gcc", "-c", "A/test.c",
				"-I", "A/B/C",
				"-ID/E/F",
				"-oA/test.o",
				"-MFtest.d",
			},
			want: []string{"test.d", "A/test.o"},
		},
		{
			desc: "with dwo",
			args: []string{
				"gcc", "-c", "A/test.c",
				"-I", "A/B/C",
				"-ID/E/F",
				"-gsplit-dwarf",
				"-o", "A/test.o",
				"-MF", "test.d",
			},
			want: []string{"test.d", "A/test.o", "A/test.dwo"},
		},
		{
			desc: "prefix with dwo",
			args: []string{
				"gcc", "-c", "A/test.c",
				"-I", "A/B/C",
				"-ID/E/F",
				"-gsplit-dwarf",
				"-oA/test.o",
				"-MFtest.d",
			},
			want: []string{"test.d", "A/test.o", "A/test.dwo"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if got := gccOutputs(tc.args); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("gccOutputs(%q)=%q; want %q", tc.args, got, tc.want)
			}
		})
	}
}
