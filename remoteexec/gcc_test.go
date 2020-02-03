// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"reflect"
	"strings"
	"testing"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
)

func TestGccCwdAgnostic(t *testing.T) {

	baseReleaseArgs := []string{
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
	}

	modifyArgs := func(args []string, prefix, replace string) []string {
		var ret []string
		found := false
		for _, arg := range args {
			if strings.HasPrefix(arg, prefix) {
				ret = append(ret, replace)
				found = true
				continue
			}
			ret = append(ret, arg)
		}
		if !found {
			ret = append(ret, replace)
		}
		return ret
	}

	for _, tc := range []struct {
		desc        string
		args        []string
		envs        []string
		cwdAgnostic bool
	}{
		{
			desc: "chromium base release",
			args: baseReleaseArgs,
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
		{
			desc: "resource-dir relative",
			args: append(append([]string{}, baseReleaseArgs...),
				"-resource-dir=../../third_party/llvm-build-release+Asserts/bin/clang/10.0.0"),
			cwdAgnostic: true,
		},
		{
			desc: "resource-dir absolute",
			args: append(append([]string{}, baseReleaseArgs...),
				"-resource-dir=/b/c/b/linux/src/third_party/llvm-build-release+Asserts/bin/clang/10.0.0"),
			cwdAgnostic: false,
		},
		{
			desc: "isystem absolute",
			args: append(append([]string{}, baseReleaseArgs...),
				"-isystem/b/c/b/linux/src/build/linux/debian_sid_amd64-sysroot/usr/include/glib-2.0"),
			cwdAgnostic: false,
		},
		{
			desc: "sysroot absolute",
			args: modifyArgs(baseReleaseArgs,
				"--sysroot",
				"--sysroot=/b/c/b/linux/build/linux/debian_sid_amd64-sysroot"),
			cwdAgnostic: false,
		},
		{
			desc: "llvm -asan option",
			// https://b/issues/141210713#comment3
			args: append(append([]string{}, baseReleaseArgs...),
				"-mllvm",
				"-asan-globals=0"),
			cwdAgnostic: true,
		},
		{
			desc: "llvm -regalloc option",
			// https://b/issues/141210713#comment4
			args: append(append([]string{}, baseReleaseArgs...),
				"-mllvm",
				"-regalloc=pbqp",
				"-mllvm",
				"-pbqp-coalescing"),
			cwdAgnostic: true,
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

func TestGccCwdAgnosticForDebugCompilationDir(t *testing.T) {
	// Tests for supporting "-fdebug-compilation-dir", see b/135719929.
	// We could have merged the cases here into TestGccCwdAgnostic, but decided
	// to separate them for clarity.

	// Do not set "-g*" options in baseReleaseArgs!
	baseReleaseArgs := []string{
		"../../third_party/llvm-build/Release+Asserts/bin/clang++",
		"../../base/time/time.cc",
	}

	// Since "-fdebug-compilation-dir" has been moved to clang driver flags in
	// https://reviews.llvm.org/D63387, we set cases both with and w/o "-Xclang"
	for _, tc := range []struct {
		desc        string
		args        []string
		envs        []string
		cwdAgnostic bool
	}{
		{
			desc: "basic",
			args: append(append([]string{}, baseReleaseArgs...),
				"-fdebug-compilation-dir",
				"."),
			cwdAgnostic: true,
		},
		{
			desc: "-Xclang",
			args: append(append([]string{}, baseReleaseArgs...),
				"-Xclang",
				"-fdebug-compilation-dir",
				"-Xclang",
				"."),
			cwdAgnostic: true,
		},
		{
			desc: "With -g* DBG options",
			args: append(append([]string{}, baseReleaseArgs...),
				"-g2",
				"-gsplit-dwarf",
				"-fdebug-compilation-dir",
				"."),
			cwdAgnostic: true,
		},
		{
			desc: "-Xclang with -g* DBG option",
			args: append(append([]string{}, baseReleaseArgs...),
				"-g2",
				"-gsplit-dwarf",
				"-Xclang",
				"-fdebug-compilation-dir",
				"-Xclang",
				"."),
			cwdAgnostic: true,
		},
		{
			// Make sure the CWD agnostic still returns false if
			// "-fdebug-compilation-dir" is not specified.
			desc: "Only -g* DBG options",
			args: append(append([]string{}, baseReleaseArgs...),
				"-g2",
				"-gsplit-dwarf"),
			cwdAgnostic: false,
		},
		{
			// "-fdebug-compilation-dir" is not supported as LLVM flags.
			desc: "No LLVM",
			args: append(append([]string{}, baseReleaseArgs...),
				"-mllvm",
				"-fdebug-compilation-dir",
				"-mllvm",
				"."),
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
