// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"reflect"
	"strings"
	"testing"

	"go.chromium.org/goma/server/command/descriptor/winpath"
)

func TestIsClangclWarningFlag(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		flags []string
		want  bool
	}{
		{
			desc: "basic",
			flags: []string{
				"/w", "/W0", "/W1", "/W2", "/W3", "/W4", "/Wall", "/WX", "/Wv",
			},
			want: true,
		},
		{
			desc: "version",
			flags: []string{
				"/Wv:foo", "/Wv:bar",
			},
			want: true,
		},
		{
			desc: "numbered warning level",
			flags: []string{
				"/w14000", "/w24001", "/wd4999", "/we5000", "/wo5001",
			},
			want: true,
		},
		{
			desc: "not warning flag",
			flags: []string{
				"/foo", "/bar", "/", "",
			},
			want: false,
		},
		{
			desc: "invalid numbered warning",
			flags: []string{
				"/wd300", "/we1000", "/wo6000", "/w1nnnn",
			},
			want: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			for _, flag := range tc.flags {
				if got := isClangclWarningFlag(flag); got != tc.want {
					t.Errorf("isClangclWarningFlag(%q)=%t; want %t", flag, got, tc.want)
				}
			}
		})
	}
}

func TestClangclRelocatableReq(t *testing.T) {

	baseReleaseArgs := []string{
		// Taken from actual Chromium Windows build args.
		"..\\..\\third_party\\llvm-build\\Release+Asserts\\bin\\clang-cl.exe",
		"/nologo",
		"/showIncludes:user",
		"-imsvc..\\..\\third_party\\depot_tools\\win_toolchain\\vs_files",
		"-DUSE_AURA=1",
		"-I../..",
		"-fcolor-diagnostics",
		"-fmerge-all-constants",
		// POSIX-style paths are sometimes passed to clang-cl in Chromium, but they are relative paths.
		"-fcrash-diagnostics-dir=../../tools/clang/crashreports",
		"-Xclang",
		"-fdebug-compilation-dir",
		"-Xclang",
		"./debug_compilation_dir",
		"-no-canonical-prefixes",
		"-I../../buildtools/third_party/libc++/trunk/include",
		"/c",
		"../../base/win/com_init_util.cc",
		"/Foobj/base/base/com_init_util.obj",
		"/Fdobj/base/base_cc.pdb",
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
		unknownFlag bool
	}{
		{
			desc: "chromium base release",
			args: baseReleaseArgs,
			envs: []string{
				"PWD=C:\\b\\c\\src\\out\\Release",
			},
			relocatable: true,
		},
		{
			desc: "clang path absolute",
			args: modifyArgs(baseReleaseArgs,
				"..\\..\\third_party\\llvm-build\\Release+Asserts\\bin\\clang-cl.exe",
				"C:\\chromium\\src\\third_party\\llvm-build\\Release+Asserts\\bin\\clang-cl.exe"),
			relocatable: false,
		},
		{
			desc: "msvc path absolute",
			args: modifyArgs(baseReleaseArgs,
				"-imsvc..\\..\\third_party\\depot_tools\\win_toolchain\\vs_files",
				"-imsvcC:\\chromium\\src\\third_party\\depot_tools\\win_toolchain\\vs_files"),
			relocatable: false,
		},
		{
			desc: "include path absolute",
			args: modifyArgs(baseReleaseArgs,
				"-I../../buildtools/third_party/libc++/trunk/include",
				"-IC:\\chromium\\src\\buildtools\\third_party\\libc++\\trunk\\include"),
			relocatable: false,
		},
		{
			desc: "-fcrash-diagnostics-dir path absolute",
			args: modifyArgs(baseReleaseArgs,
				"-fcrash-diagnostics-dir=../../tools/clang/crashreports",
				"-fcrash-diagnostics-dir=C:\\chromium\\src\\tools\\clang\\crashreports"),
			relocatable: false,
		},
		{
			desc: "output path absolute",
			args: modifyArgs(baseReleaseArgs,
				"/Foobj/base/base/com_init_util.obj",
				"/FoC:\\chromium\\src\\obj\\base\\base\\com_init_util.obj"),
			relocatable: false,
		},
		{
			desc: "debug output path absolute",
			args: modifyArgs(baseReleaseArgs,
				"/Fdobj/base/base_cc.pdb",
				"/FdC:\\chromium\\src\\obj\\base\\base_cc.pdb"),
			relocatable: false,
		},
		{
			desc: "source path absolute",
			args: modifyArgs(baseReleaseArgs,
				"../../base/win/com_init_util.cc",
				"C:\\chromium\\src\\base\\win\\com_init_util.cc"),
			relocatable: false,
		},
		{
			desc: "-fdebug-compilation-dir",
			args: modifyArgs(baseReleaseArgs,
				"./debug_compilation_dir",
				"C:\\chromium\\src\\out\\Release\\debug_compilation_dir"),
			relocatable: false,
		},
		{
			desc:        "invalid msvc flag",
			args:        modifyArgs(baseReleaseArgs, "", "/invalid"),
			relocatable: false,
			unknownFlag: true,
		},
		{
			desc:        "invalid dash flag",
			args:        modifyArgs(baseReleaseArgs, "", "-invalid"),
			relocatable: false,
			unknownFlag: true,
		},
		{
			desc:        "non-scoped showIncludes",
			args:        modifyArgs(baseReleaseArgs, "/showIncludes:user", "/showIncludes"),
			relocatable: true,
			unknownFlag: false,
		},
		{
			desc:        "full chromium release build args",
			args:        fullChromiumReleaseBuildArgs,
			relocatable: true,
		},
		{
			desc:        "full chromium debug build args",
			args:        fullChromiumDebugBuildArgs,
			relocatable: true,
		},
		{
			desc:        "full llvm release build args",
			args:        fullLLVMReleaseBuildArgs,
			relocatable: false,
		},
		{
			desc: "-mllvm -instcombine-lower-dbg-declare=0",
			args: append(append([]string{}, baseReleaseArgs...),
				"-mllvm", "-instcombine-lower-dbg-declare=0"),
			relocatable: true,
		},
		{
			desc: "-mllvm -basic-aa-recphi=0",
			args: append(append([]string{}, baseReleaseArgs...),
				"-mllvm", "-basic-aa-recphi=0"),
			relocatable: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			err := clangclRelocatableReq(winpath.FilePath{}, tc.args, tc.envs)
			if (err == nil) != tc.relocatable {
				t.Errorf("clangclRelocatableReq(winpath.FilePath, args, envs)=%v; relocatable=%t", err, tc.relocatable)
			}
			if err != nil && tc.unknownFlag != strings.Contains(err.Error(), "unknown flag") {
				t.Errorf("clangclRelocatableReq(winpath.FilePath, args, envs)=%v; expected unknown flag", err)
			}
		})
	}
}

func TestClangclOutputs(t *testing.T) {
	for _, tc := range []struct {
		desc string
		args []string
		want []string
	}{
		{
			desc: "basic",
			args: []string{
				"clang-cl", "/c", "A/test.c",
				"/I", "A/B/C",
				"/ID/E/F",
				"/o", "A/test.o",
			},
			want: []string{"A/test.o"},
		},
		{
			desc: "basic /Fo /Fd",
			args: []string{
				"clang-cl", "/c", "A/test.c",
				"/I", "A/B/C",
				"/ID/E/F",
				"/FoA/test.o",
				"/FdA/test.pdb",
			},
			// TODO: capture pdb.
			want: []string{"A/test.o"},
		},
		{
			desc: "basic dash",
			args: []string{
				"clang-cl", "-c", "A/test.c",
				"-I", "A/B/C",
				"-ID/E/F",
				"-o", "A/test.o",
			},
			want: []string{"A/test.o"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if got := clangclOutputs(tc.args); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("clangclOutputs(%q)=%q; want %q", tc.args, got, tc.want)
			}
		})
	}
}

var fullChromiumReleaseBuildArgs = []string{
	"..\\..\\third_party\\llvm-build\\Release+Asserts\\bin\\clang-cl.exe",
	"/nologo",
	"/showIncludes:user",
	"-imsvc..\\..\\third_party\\depot_tools\\win_toolchain\\vs_files\\9ff60e43ba91947baca460d0ca3b1b980c3a2c23\\win_sdk",
	"-DUSE_AURA=1",
	"-DCR_CLANG_REVISION=\"n353803-99ac9ce7-1\"",
	"-D_HAS_NODISCARD",
	"-D_LIBCPP_ABI_UNSTABLE",
	"-D_LIBCPP_DISABLE_VISIBILITY_ANNOTATIONS",
	"-D_LIBCPP_ENABLE_NODISCARD",
	"-D_LIBCPP_NO_AUTO_LINK",
	"-D__STD_C",
	"-D_CRT_RAND_S",
	"-D_CRT_SECURE_NO_DEPRECATE",
	"-D_SCL_SECURE_NO_DEPRECATE",
	"-D_ATL_NO_OPENGL",
	"-D_WINDOWS",
	"-DCERT_CHAIN_PARA_HAS_EXTRA_FIELDS",
	"-DPSAPI_VERSION=2",
	"-DWIN32",
	"-D_SECURE_ATL",
	"-DWINAPI_FAMILY=WINAPI_FAMILY_DESKTOP_APP",
	"-DWIN32_LEAN_AND_MEAN",
	"-DNOMINMAX",
	"-D_UNICODE",
	"-DUNICODE",
	"-DNTDDI_VERSION=NTDDI_WIN10_RS2",
	"-D_WIN32_WINNT=0x0A00",
	"-DWINVER=0x0A00",
	"-DNDEBUG",
	"-DNVALGRIND",
	"-DDYNAMIC_ANNOTATIONS_ENABLED=0",
	"-DBASE_IMPLEMENTATION",
	"-I../..",
	"-Igen",
	"-I../../third_party/boringssl/src/include",
	"-fcolor-diagnostics",
	"-fmerge-all-constants",
	"-fcrash-diagnostics-dir=../../tools/clang/crashreports",
	"-Xclang",
	"-mllvm",
	"-Xclang",
	"-instcombine-lower-dbg-declare=0",
	"-fcomplete-member-pointers",
	"/Gy",
	"/FS",
	"/bigobj",
	"/utf-8",
	"/Zc:twoPhase",
	"/Zc:sizedDealloc-",
	"/X",
	"-fmsc-version=1916",
	"/guard:cf,nochecks",
	"-m64",
	"/Brepro",
	"-Wno-builtin-macro-redefined",
	"-D__DATE__=",
	"-D__TIME__=",
	"-D__TIMESTAMP__=",
	"-Xclang",
	"-fdebug-compilation-dir",
	"-Xclang",
	".",
	"-no-canonical-prefixes",
	"/W4",
	"-Wimplicit-fallthrough",
	"-Wunreachable-code",
	"-Wthread-safety",
	"-Wextra-semi",
	"/WX",
	"/wd4091",
	"/wd4127",
	"/wd4251",
	"/wd4275",
	"/wd4312",
	"/wd4324",
	"/wd4351",
	"/wd4355",
	"/wd4503",
	"/wd4589",
	"/wd4611",
	"/wd4100",
	"/wd4121",
	"/wd4244",
	"/wd4505",
	"/wd4510",
	"/wd4512",
	"/wd4610",
	"/wd4838",
	"/wd4995",
	"/wd4996",
	"/wd4456",
	"/wd4457",
	"/wd4458",
	"/wd4459",
	"/wd4200",
	"/wd4201",
	"/wd4204",
	"/wd4221",
	"/wd4245",
	"/wd4267",
	"/wd4305",
	"/wd4389",
	"/wd4702",
	"/wd4701",
	"/wd4703",
	"/wd4661",
	"/wd4706",
	"/wd4715",
	"-Wno-missing-field-initializers",
	"-Wno-unused-parameter",
	"-Wno-c++11-narrowing",
	"-Wno-unneeded-internal-declaration",
	"-Wno-undefined-var-template",
	"-Wno-nonportable-include-path",
	"-Wno-ignored-pragma-optimize",
	"-Wno-implicit-int-float-conversion",
	"-Wno-final-dtor-non-final-class",
	"-Wno-builtin-assume-aligned-alignment",
	"-Wno-deprecated-copy",
	"-Wno-non-c-typedef-for-linkage",
	"-Wmax-tokens",
	"/Z7",
	"-gcodeview-ghash",
	"-Xclang",
	"-debug-info-kind=constructor",
	"-ftrivial-auto-var-init=pattern",
	"/MT",
	"-Xclang",
	"-add-plugin",
	"-Xclang",
	"find-bad-constructs",
	"-Wheader-hygiene",
	"-Wstring-conversion",
	"-Wtautological-overlap-compare",
	"-Wglobal-constructors",
	"-Wexit-time-destructors",
	"-Wshadow",
	"-Wno-shorten-64-to-32",
	"-Wexit-time-destructors",
	"/O2",
	"/Ob2",
	"/Oy-",
	"/Zc:inline",
	"/Gw",
	"/TP",
	"/wd4577",
	"/GR-",
	"-I../../buildtools/third_party/libc++/trunk/include",
	"/c",
	"../../base/win/com_init_util.cc",
	"/Foobj/base/base/com_init_util.obj",
	"/Fdobj/base/base_cc.pdb",
}

var fullChromiumDebugBuildArgs = []string{
	"..\\..\\third_party\\llvm-build\\Release+Asserts\\bin\\clang-cl.exe",
	"/nologo",
	"/showIncludes:user",
	"-imsvc..\\..\\third_party\\depot_tools\\win_toolchain\\vs_files\\9ff60e43ba91947baca460d0ca3b1b980c3a2c23\\win_sdk",
	"-DUSE_AURA=1",
	"-DCR_CLANG_REVISION=\"n353803-99ac9ce7-1\"",
	"-D_HAS_NODISCARD",
	"-DCOMPONENT_BUILD",
	"-D_LIBCPP_ABI_UNSTABLE",
	"-D_LIBCPP_ENABLE_NODISCARD",
	"-D_LIBCPP_NO_AUTO_LINK",
	"-D__STD_C",
	"-D_CRT_RAND_S",
	"-D_CRT_SECURE_NO_DEPRECATE",
	"-D_SCL_SECURE_NO_DEPRECATE",
	"-D_ATL_NO_OPENGL",
	"-D_WINDOWS",
	"-DCERT_CHAIN_PARA_HAS_EXTRA_FIELDS",
	"-DPSAPI_VERSION=2",
	"-DWIN32",
	"-D_SECURE_ATL",
	"-DWINAPI_FAMILY=WINAPI_FAMILY_DESKTOP_APP",
	"-DWIN32_LEAN_AND_MEAN",
	"-DNOMINMAX",
	"-D_UNICODE",
	"-DUNICODE",
	"-DNTDDI_VERSION=NTDDI_WIN10_RS2",
	"-D_WIN32_WINNT=0x0A00",
	"-DWINVER=0x0A00",
	"-D_DEBUG",
	"-DDYNAMIC_ANNOTATIONS_ENABLED=1",
	"-I../..",
	"-Igen",
	"-fcolor-diagnostics",
	"-fmerge-all-constants",
	"-fcrash-diagnostics-dir=../../tools/clang/crashreports",
	"-Xclang",
	"-mllvm",
	"-Xclang",
	"-instcombine-lower-dbg-declare=0",
	"-fcomplete-member-pointers",
	"/Gy",
	"/FS",
	"/bigobj",
	"/utf-8",
	"/Zc:twoPhase",
	"/Zc:sizedDealloc-",
	"/X",
	"-fmsc-version=1916",
	"/guard:cf,nochecks",
	"/Zc:dllexportInlines-",
	"-m64",
	"/Brepro",
	"-Wno-builtin-macro-redefined",
	"-D__DATE__=",
	"-D__TIME__=",
	"-D__TIMESTAMP__=",
	"-Xclang",
	"-fdebug-compilation-dir",
	"-Xclang",
	".",
	"-no-canonical-prefixes",
	"/W4",
	"-Wimplicit-fallthrough",
	"-Wunreachable-code",
	"-Wthread-safety",
	"-Wextra-semi",
	"/WX",
	"/wd4091",
	"/wd4127",
	"/wd4251",
	"/wd4275",
	"/wd4312",
	"/wd4324",
	"/wd4351",
	"/wd4355",
	"/wd4503",
	"/wd4589",
	"/wd4611",
	"/wd4100",
	"/wd4121",
	"/wd4244",
	"/wd4505",
	"/wd4510",
	"/wd4512",
	"/wd4610",
	"/wd4838",
	"/wd4995",
	"/wd4996",
	"/wd4456",
	"/wd4457",
	"/wd4458",
	"/wd4459",
	"/wd4200",
	"/wd4201",
	"/wd4204",
	"/wd4221",
	"/wd4245",
	"/wd4267",
	"/wd4305",
	"/wd4389",
	"/wd4702",
	"/wd4701",
	"/wd4703",
	"/wd4661",
	"/wd4706",
	"/wd4715",
	"-Wno-missing-field-initializers",
	"-Wno-unused-parameter",
	"-Wno-c++11-narrowing",
	"-Wno-unneeded-internal-declaration",
	"-Wno-undefined-var-template",
	"-Wno-nonportable-include-path",
	"-Wno-ignored-pragma-optimize",
	"-Wno-implicit-int-float-conversion",
	"-Wno-final-dtor-non-final-class",
	"-Wno-builtin-assume-aligned-alignment",
	"-Wno-deprecated-copy",
	"-Wno-non-c-typedef-for-linkage",
	"-Wmax-tokens",
	"/Od",
	"/Ob0",
	"/GF",
	"/Z7",
	"-gcodeview-ghash",
	"-Xclang",
	"-debug-info-kind=constructor",
	"-ftrivial-auto-var-init=pattern",
	"/MDd",
	"-Xclang",
	"-add-plugin",
	"-Xclang",
	"find-bad-constructs",
	"-Wheader-hygiene",
	"-Wstring-conversion",
	"-Wtautological-overlap-compare",
	"-Wno-unused-const-variable",
	"-Wno-unused-function",
	"-Wno-undefined-bool-conversion",
	"-Wno-tautological-undefined-compare",
	"/TP",
	"/wd4577",
	"/GR-",
	"-I../../buildtools/third_party/libc++/trunk/include",
	"/c",
	"../../base/third_party/double_conversion/double-conversion/fast-dtoa.cc",
	"/Foobj/base/third_party/double_conversion/double_conversion/fast-dtoa.obj",
	"/Fdobj/base/third_party/double_conversion/double_conversion_cc.pdb",
}

var fullLLVMReleaseBuildArgs = []string{
	"C:\\b\\s\\w\\ir\\cache\\builder\\v8\\third_party\\llvm-build\\Release+Asserts\\bin\\clang-cl.exe",
	"/nologo",
	"-TP",
	"-DGTEST_HAS_RTTI=0",
	"-DUNICODE",
	"-D_CRT_NONSTDC_NO_DEPRECATE",
	"-D_CRT_NONSTDC_NO_WARNINGS",
	"-D_CRT_SECURE_NO_DEPRECATE",
	"-D_CRT_SECURE_NO_WARNINGS",
	"-D_HAS_EXCEPTIONS=0",
	"-D_SCL_SECURE_NO_DEPRECATE",
	"-D_SCL_SECURE_NO_WARNINGS",
	"-D_UNICODE",
	"-D__STDC_CONSTANT_MACROS",
	"-D__STDC_FORMAT_MACROS",
	"-D__STDC_LIMIT_MACROS",
	"-Itools\\opt",
	"-IC:\\b\\s\\w\\ir\\cache\\builder\\emscripten-releases\\llvm-project\\llvm\\tools\\opt",
	"-Iinclude",
	"-IC:\\b\\s\\w\\ir\\cache\\builder\\emscripten-releases\\llvm-project\\llvm\\include",
	"-Wno-nonportable-include-path",
	"/Zc:inline",
	"/Zc:strictStrings",
	"/Oi",
	"/Zc:rvalueCast",
	"/Brepro",
	"/W4",
	"-Wextra",
	"-Wno-unused-parameter",
	"-Wwrite-strings",
	"-Wcast-qual",
	"-Wmissing-field-initializers",
	"-Wimplicit-fallthrough",
	"-Wcovered-switch-default",
	"-Wno-noexcept-type",
	"-Wdelete-non-virtual-dtor",
	"-Wstring-conversion",
	"/Gw",
	"/MD",
	"/O2",
	"/Ob2",
	"/EHs-c-",
	"/GR-",
	"-UNDEBUG",
	"-std:c++14",
	"/showIncludes",
	"/Fotools\\opt\\CMakeFiles\\opt.dir\\PrintSCC.cpp.obj",
	"/Fdtools\\opt\\CMakeFiles\\opt.dir\\",
	"-c",
	"C:\\b\\s\\w\\ir\\cache\\builder\\emscripten-releases\\llvm-project\\llvm\\tools\\opt\\PrintSCC.cpp",
}
