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

func TestGccRelocatableReq(t *testing.T) {

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
		relocatable bool
	}{
		{
			desc: "chromium base release",
			args: baseReleaseArgs,
			envs: []string{
				"PWD=/b/c/b/linux/src/out/Release",
			},
			relocatable: true,
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
			relocatable: false,
		},
		{
			desc: "resource-dir relative",
			args: append(append([]string{}, baseReleaseArgs...),
				"-resource-dir=../../third_party/llvm-build-release+Asserts/bin/clang/10.0.0"),
			relocatable: true,
		},
		{
			desc: "resource-dir absolute",
			args: append(append([]string{}, baseReleaseArgs...),
				"-resource-dir=/b/c/b/linux/src/third_party/llvm-build-release+Asserts/bin/clang/10.0.0"),
			relocatable: false,
		},
		{
			desc: "isystem absolute",
			args: append(append([]string{}, baseReleaseArgs...),
				"-isystem/b/c/b/linux/src/build/linux/debian_sid_amd64-sysroot/usr/include/glib-2.0"),
			relocatable: false,
		},
		{
			desc: "include absolute",
			args: append(append([]string{}, baseReleaseArgs...),
				"-include",
				"/b/c/b/linux/header.h"),
			relocatable: false,
		},
		{
			desc: "include relative",
			args: append(append([]string{}, baseReleaseArgs...),
				"-include",
				"../b/c/b/linux/header.h"),
			relocatable: true,
		},
		{
			desc: "include= absolute",
			args: append(append([]string{}, baseReleaseArgs...),
				"-include=/b/c/b/linux/header.h"),
			relocatable: false,
		},
		{
			desc: "include= relative",
			args: append(append([]string{}, baseReleaseArgs...),
				"-include=../b/c/b/linux/header.h"),
			relocatable: true,
		},
		{
			desc: "sysroot absolute",
			args: modifyArgs(baseReleaseArgs,
				"--sysroot",
				"--sysroot=/b/c/b/linux/build/linux/debian_sid_amd64-sysroot"),
			relocatable: false,
		},
		{
			desc: "llvm -asan option",
			// https://b/issues/141210713#comment3
			args: append(append([]string{}, baseReleaseArgs...),
				"-mllvm",
				"-asan-globals=0"),
			relocatable: true,
		},
		{
			desc: "llvm -regalloc option",
			// https://b/issues/141210713#comment4
			args: append(append([]string{}, baseReleaseArgs...),
				"-mllvm",
				"-regalloc=pbqp",
				"-mllvm",
				"-pbqp-coalescing"),
			relocatable: true,
		},
		{
			desc: "headermap file",
			// http://b/149448356#comment17
			args: append(append([]string{}, baseReleaseArgs...),
				"-Ifoo.hmap"),
			relocatable: false,
		},
		{
			desc: "headermap file 2",
			// http://b/149448356#comment17
			args: append(append([]string{}, baseReleaseArgs...),
				[]string{"-I", "foo.hmap"}...),
			relocatable: false,
		},
		{
			desc: "Qunused-arguments",
			args: append(append([]string{}, baseReleaseArgs...),
				"-Qunused-arguments"),
			relocatable: true,
		},
		{
			desc: "fprofile-instr-use relative",
			args: append(append([]string{}, baseReleaseArgs...),
				"-fprofile-instr-use=../../out/data/default.profdata"),
			relocatable: true,
		},
		{
			desc: "fprofile-instr-use absolute",
			args: append(append([]string{}, baseReleaseArgs...),
				"-fprofile-instr-use=/b/c/b/linux/src/out/data/default.profdataa"),
			relocatable: false,
		},
		{
			desc: "fprofile-sample-use relative",
			args: append(append([]string{}, baseReleaseArgs...),
				"-fprofile-sample-use=../../chrome/android/profiles/afdo.prof"),
			relocatable: true,
		},
		{
			desc: "fprofile-sample-use absolute",
			args: append(append([]string{}, baseReleaseArgs...),
				"-fprofile-sample-use=/b/c/b/linux/src/android/profiles/afdo.prof"),
			relocatable: false,
		},
		{
			desc: "fsatinitize-blacklist relative",
			args: append(append([]string{}, baseReleaseArgs...),
				"-fsanitize-blacklist=../../tools/cfi/blacklist.txt"),
			relocatable: true,
		},
		{
			desc: "fsanitize-blacklist absolute",
			args: append(append([]string{}, baseReleaseArgs...),
				"-fsanitize-blacklist=/b/c/b/linux/src/tools/cfi/blacklist.txt"),
			relocatable: false,
		},
		{
			desc: "fcrash-diagnostics-dir absolute",
			args: append(append([]string{}, baseReleaseArgs...),
				"-fcrash-diagnostics-dir=../../tools/clang/crashreports"),
			relocatable: true,
		},
		{
			desc: "fcrash-diagnostics-dir absolute",
			args: append(append([]string{}, baseReleaseArgs...),
				"-fcrash-diagnostics-dir=/b/c/b/linux/src/tools/clang/crashreports"),
			relocatable: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			err := gccRelocatableReq(posixpath.FilePath{}, tc.args, tc.envs)
			if (err == nil) != tc.relocatable {
				t.Errorf("gccRelocatableReq(posixpath.FilePath, args, envs)=%v; relocatable=%t", err, tc.relocatable)
			}
		})
	}
}

func TestGccRelocatableReqForDebugCompilationDir(t *testing.T) {
	// Tests for supporting "-fdebug-compilation-dir", see b/135719929.
	// We could have merged the cases here into TestGccRelocatableReq, but decided
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
		relocatable bool
	}{
		{
			desc: "basic",
			args: append(append([]string{}, baseReleaseArgs...),
				"-fdebug-compilation-dir",
				"."),
			relocatable: true,
		},
		{
			desc: "-Xclang",
			args: append(append([]string{}, baseReleaseArgs...),
				"-Xclang",
				"-fdebug-compilation-dir",
				"-Xclang",
				"."),
			relocatable: true,
		},
		{
			desc: "With -g* DBG options",
			args: append(append([]string{}, baseReleaseArgs...),
				"-g2",
				"-gsplit-dwarf",
				"-fdebug-compilation-dir",
				"."),
			relocatable: true,
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
			relocatable: true,
		},
		{
			// Make sure the CWD agnostic still returns false if
			// "-fdebug-compilation-dir" is not specified.
			desc: "Only -g* DBG options",
			args: append(append([]string{}, baseReleaseArgs...),
				"-g2",
				"-gsplit-dwarf"),
			relocatable: false,
		},
		{
			// "-fdebug-compilation-dir" is not supported as LLVM flags.
			desc: "No LLVM",
			args: append(append([]string{}, baseReleaseArgs...),
				"-mllvm",
				"-fdebug-compilation-dir",
				"-mllvm",
				"."),
			relocatable: false,
		},
		{
			desc: "input abs path",
			args: append(append([]string{}, baseReleaseArgs...),
				"-fdebug-compilation-dir",
				".",
				"-isystem/b/c/b/linux/src/build/linux/debian_sid_amd64-sysroot/usr/include/glib-2.0"),
			relocatable: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			err := gccRelocatableReq(posixpath.FilePath{}, tc.args, tc.envs)
			if (err == nil) != tc.relocatable {
				t.Errorf("gccRelocatableReq(posixpath.FilePath, args, envs)=%v; relocatable=%t", err, tc.relocatable)
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
