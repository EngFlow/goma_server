// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/hash"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	"go.chromium.org/goma/server/remoteexec/digest"
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

func TestExtractLLVMError(t *testing.T) {
	// from fdf0c2b3d3d633787324c90ff55404cd0f45fa83cd48e033c332961c9e13e7b5-277800
	// in http://b/145177862
	msg := []byte(`Note: including file: ../..\content/renderer/render_frame_impl.h
Note: including file:  ../../buildtools/third_party/libc++/trunk/include\stddef.h
Note: including file:   ../../buildtools/third_party/libc++/trunk/include\__config
Note: including file:  ../..\base/native_library.h
Note: including file:  ../..\ppapi/c/ppb_core.h
Note: including file:  ../..\ppapi/shared_impl/ppapi_permissions.h
Note: including file: ../..\base/debug/invalid_access_win.h
Note: including file: ../..\base/process/kill.h
LLVM ERROR: out of memory
Stack dump:
0.	Program arguments: ..\..\third_party\llvm-build\Release+Asserts\bin\clang-cl.exe -cc1 -triple i386-pc-windows-msvc19.16.0 -emit-obj -disable-free -disable-llvm-verifier -discard-value-names -main-file-name render_frame_impl.cc -mrelocation-model static -mthread-model posix -fmerge-all-constants -mframe-pointer=all -relaxed-aliasing -fmath-errno -fno-rounding-math -masm-verbose -mconstructor-aliases -target-cpu pentium4 -mllvm -x86-asm-syntax=intel -D_MT -flto-visibility-public-std --dependent-lib=libcmt --dependent-lib=oldnames --show-includes -fno-rtti-data -stack-protector 2 -fms-volatile -fdiagnostics-format msvc -cfguard-no-checks -gcodeview -debug-info-kind=line-tables-only -ffunction-sections -fdata-sections -nostdsysteminc -resource-dir ..\..\third_party\llvm-build\Release+Asserts\lib\clang\10.0.0 -D USE_AURA=1 -D CR_CLANG_REVISION="n331734-e84b7a5f-1" -D _HAS_NODISCARD -D _LIBCPP_ABI_UNSTABLE -D _LIBCPP_DISABLE_VISIBILITY_ANNOTATIONS -D _LIBCPP_ENABLE_NODISCARD -D _LIBCPP_NO_AUTO_LINK -D __STD_C -D _CRT_RAND_S -D _CRT_SECURE_NO_DEPRECATE -D _SCL_SECURE_NO_DEPRECATE -D _ATL_NO_OPENGL -D _WINDOWS -D CERT_CHAIN_PARA_HAS_EXTRA_FIELDS -D PSAPI_VERSION=2 -D WIN32 -D _SECURE_ATL -D _USING_V110_SDK71_ -D WINAPI_FAMILY=WINAPI_FAMILY_DESKTOP_APP -D WIN32_LEAN_AND_MEAN -D NOMINMAX -D _UNICODE -D UNICODE -D NTDDI_VERSION=NTDDI_WIN10_RS2 -D _WIN32_WINNT=0x0A00 -D WINVER=0x0A00 -D NDEBUG -D NVALGRIND -D DYNAMIC_ANNOTATIONS_ENABLED=0 -D CONTENT_IMPLEMENTATION -D ENABLE_IPC_FUZZER -D WEBP_EXTERN=extern -D USE_EGL -D _WTL_NO_AUTOMATIC_NAMESPACE -D U_USING_ICU_NAMESPACE=0 -D U_ENABLE_DYLOAD=0 -D USE_CHROMIUM_ICU=1 -D U_STATIC_IMPLEMENTATION -D ICU_UTIL_DATA_IMPL=ICU_UTIL_DATA_FILE -D UCHAR_TYPE=wchar_t -D GOOGLE_PROTOBUF_NO_RTTI -D GOOGLE_PROTOBUF_NO_STATIC_INITIALIZER -D WEBRTC_NON_STATIC_TRACE_EVENT_HANDLERS=0 -D WEBRTC_CHROMIUM_BUILD -D WEBRTC_WIN -D ABSL_ALLOCATOR_NOTHROW=1 -D WEBRTC_USE_BUILTIN_ISAC_FIX=0 -D WEBRTC_USE_BUILTIN_ISAC_FLOAT=1 -D HAVE_SCTP -D NO_MAIN_THREAD_WRAPPING -D SK_HAS_PNG_LIBRARY -D SK_HAS_WEBP_LIBRARY -D SK_USER_CONFIG_HEADER="../../skia/config/SkUserConfig.h" -D SK_GL -D SK_HAS_JPEG_LIBRARY -D SK_HAS_WUFFS_LIBRARY -D SK_SUPPORT_GPU=1 -D SK_GPU_WORKAROUNDS_HEADER="gpu/config/gpu_driver_bug_workaround_autogen.h" -D GR_GL_FUNCTION_TYPE=__stdcall -D LEVELDB_PLATFORM_CHROMIUM=1 -D LEVELDB_PLATFORM_CHROMIUM=1 -D V8_USE_EXTERNAL_STARTUP_DATA -D V8_31BIT_SMIS_ON_64BIT_ARCH -D V8_DEPRECATION_WARNINGS -D SUPPORT_WEBGL2_COMPUTE_CONTEXT=1 -D WTF_USE_WEBAUDIO_PFFFT=1 -D V8_31BIT_SMIS_ON_64BIT_ARCH -D V8_DEPRECATION_WARNINGS -D AUDIO_PROCESSING_IN_AUDIO_SERVICE -D SQLITE_ENABLE_BATCH_ATOMIC_WRITE -D SQLITE_ENABLE_FTS3 -D SQLITE_DISABLE_FTS3_UNICODE -D SQLITE_DISABLE_FTS4_DEFERRED -D SQLITE_ENABLE_ICU -D SQLITE_SECURE_DELETE -D SQLITE_THREADSAFE=1 -D SQLITE_MAX_WORKER_THREADS=0 -D SQLITE_MAX_MMAP_SIZE=268435456 -D SQLITE_DEFAULT_FILE_PERMISSIONS=0600 -D SQLITE_DEFAULT_MEMSTATUS=1 -D SQLITE_DEFAULT_PAGE_SIZE=4096 -D SQLITE_DEFAULT_PCACHE_INITSZ=0 -D SQLITE_LIKE_DOESNT_MATCH_BLOBS -D SQLITE_OMIT_DEPRECATED -D SQLITE_OMIT_PROGRESS_CALLBACK -D SQLITE_OMIT_SHARED_CACHE -D SQLITE_USE_ALLOCA -D SQLITE_OMIT_ANALYZE -D SQLITE_OMIT_AUTOINIT -D SQLITE_OMIT_AUTORESET -D SQLITE_OMIT_COMPILEOPTION_DIAGS -D SQLITE_OMIT_COMPLETE -D SQLITE_OMIT_DECLTYPE -D SQLITE_OMIT_EXPLAIN -D SQLITE_OMIT_GET_TABLE -D SQLITE_OMIT_LOAD_EXTENSION -D SQLITE_DEFAULT_LOOKASIDE=0,0 -D SQLITE_OMIT_LOOKASIDE -D SQLITE_OMIT_TCL_VARIABLE -D SQLITE_OMIT_REINDEX -D SQLITE_OMIT_TRACE -D SQLITE_OMIT_UPSERT -D SQLITE_OMIT_WINDOWFUNC -D SQLITE_HAVE_ISNAN -D SQLITE_TEMP_STORE=3 -D SQLITE_ENABLE_LOCKING_STYLE=0 -I ../.. -I gen -I ../../third_party/libyuv/include -I ../../third_party/jsoncpp/source/include -I ../../third_party/jsoncpp/generated -I ../../third_party/libwebp/src -I ../../third_party/wtl/include -I ../../third_party/khronos -I ../../gpu -I gen/third_party/dawn/src/include -I ../../third_party/dawn/src/include -I ../../third_party/boringssl/src/include -I ../../third_party/ced/src -I ../../third_party/icu/source/common -I ../../third_party/icu/source/i18n -I ../../third_party/protobuf/src -I gen/protoc_out -I ../../third_party/protobuf/src -I ../../third_party/webrtc_overrides -I ../../third_party/webrtc -I gen/third_party/webrtc -I ../../third_party/abseil-cpp -I ../../third_party/skia -I ../../third_party/wuffs/src/release/c -I ../../third_party/libwebm/source -I ../../third_party/leveldatabase -I ../../third_party/leveldatabase/src -I ../../third_party/leveldatabase/src/include -I ../../v8/include -I gen/v8/include -I gen/third_party/metrics_proto -I ../../third_party/mesa_headers -I ../../v8/include -I gen/v8/include -I ../../third_party/perfetto/include -I gen/third_party/perfetto/build_config -I gen/third_party/perfetto -I gen/third_party/perfetto -I gen/third_party/perfetto -I gen/third_party/perfetto -I gen/third_party/perfetto -I gen/third_party/perfetto -I gen/third_party/perfetto -I gen/third_party/perfetto -I gen/third_party/perfetto -I gen/third_party/perfetto -I ../../third_party/libvpx/source/libvpx -I ../../third_party/opus/src/include -D __DATE__= -D __TIME__= -D __TIMESTAMP__= -I ../../buildtools/third_party/libc++/trunk/include -internal-isystem ..\..\third_party\llvm-build\Release+Asserts\lib\clang\10.0.0\include -internal-isystem ..\..\third_party\depot_tools\win_toolchain\vs_files\8f58c55897a3282ed617055775a77ec3db771b88\win_sdk\Include\10.0.18362.0\um -internal-isystem ..\..\third_party\depot_tools\win_toolchain\vs_files\8f58c55897a3282ed617055775a77ec3db771b88\win_sdk\Include\10.0.18362.0\shared -internal-isystem ..\..\third_party\depot_tools\win_toolchain\vs_files\8f58c55897a3282ed617055775a77ec3db771b88\win_sdk\Include\10.0.18362.0\winrt -internal-isystem ..\..\third_party\depot_tools\win_toolchain\vs_files\8f58c55897a3282ed617055775a77ec3db771b88\win_sdk\Include\10.0.18362.0\ucrt -internal-isystem ..\..\third_party\depot_tools\win_toolchain\vs_files\8f58c55897a3282ed617055775a77ec3db771b88\VC\Tools\MSVC\14.23.28105\include -internal-isystem ..\..\third_party\depot_tools\win_toolchain\vs_files\8f58c55897a3282ed617055775a77ec3db771b88\VC\Tools\MSVC\14.23.28105\atlmfc\include -Os -Wno-builtin-macro-redefined -WCL4 -Wimplicit-fallthrough -Wthread-safety -Wextra-semi -Werror -Wno-unused-parameter -Wno-deprecated-declarations -Wno-missing-field-initializers -Wno-unused-parameter -Wno-c++11-narrowing -Wno-unneeded-internal-declaration -Wno-undefined-var-template -Wno-nonportable-include-path -Wno-ignored-pragma-optimize -Wno-implicit-int-float-conversion -Wno-c99-designator -Wno-final-dtor-non-final-class -Wno-sizeof-array-div -Wno-bitwise-conditional-parentheses -Wno-builtin-assume-aligned-alignment -Wno-tautological-bitwise-compare -Wheader-hygiene -Wstring-conversion -Wtautological-overlap-compare -Wshadow -Wexit-time-destructors -Wno-deprecated-declarations -Wno-deprecated-declarations -fdeprecated-macro -fdebug-compilation-dir C:\botcode\w\out\Release -ferror-limit 19 -fmessage-length 0 -fno-use-cxa-atexit -fms-extensions -fms-compatibility -fms-compatibility-version=19.16 -std=c++14 -finline-functions -fobjc-runtime=gcc -fdiagnostics-show-option -fcolor-diagnostics -vectorize-loops -vectorize-slp -mllvm -instcombine-lower-dbg-declare=0 -fdebug-compilation-dir . -add-plugin find-bad-constructs -add-plugin blink-gc-plugin -plugin-arg-blink-gc-plugin no-gc-finalized -fcomplete-member-pointers -faddrsig -o obj/content/renderer/renderer/render_frame_impl.obj -x c++ ../../content/renderer/render_frame_impl.cc
1.	<eof> parser at end of file
2.	Per-file LLVM IR generation
3.	../../buildtools/third_party/libc++/trunk/include\vector:353:14: Generating code for declaration 'std::__1::__vector_base<content::FaviconURL, std::__1::allocator<content::FaviconURL> >::__end_cap'
0x00007FF633872FE6 (0x00007FF636C42730 0x00007FF635EB0D63 0x000000EB03B89D98 0x000000000000001A) <unknown module>
0x00007FF635EB0C19 (0x00007FF635EB0D63 0x000000EB03B89D98 0x000000000000001A 0x00007FF636C42B30) <unknown module>
0x00007FF636C42730 (0x000000EB03B89D98 0x000000000000001A 0x00007FF636C42B30 0x0000000000000012) <unknown module>
0x00007FF635EB0D63 (0x000000000000001A 0x00007FF636C42B30 0x0000000000000012 0x0000000000000000) <unknown module>
0x000000EB03B89D98 (0x00007FF636C42B30 0x0000000000000012 0x0000000000000000 0x00007FF635EB080D) <unknown module>
0x000000000000001A (0x0000000000000012 0x0000000000000000 0x00007FF635EB080D 0x0000023A0CCA2868) <unknown module>
0x00007FF636C42B30 (0x0000000000000000 0x00007FF635EB080D 0x0000023A0CCA2868 0x000000EB03B8A0C0) <unknown module>
0x0000000000000012 (0x00007FF635EB080D 0x0000023A0CCA2868 0x000000EB03B8A0C0 0x0000000000000000) <unknown module>
clang-cl: error: clang frontend command failed due to signal (use -v to see invocation)
clang version 10.0.0 (https://github.com/llvm/llvm-project/ e84b7a5fe230e42b8e6fe451369874a773bf1867)
Target: i386-pc-windows-msvc
Thread model: posix
InstalledDir: ..\..\third_party\llvm-build\Release+Asserts\bin
clang-cl: note: diagnostic msg: PLEASE submit a bug report to https://crbug.com and run tools/clang/scripts/process_crashreports.py (only works inside Google) which will upload a report and include the crash backtrace, preprocessed source, and associated run script.
clang-cl: note: diagnostic msg:
********************

PLEASE ATTACH THE FOLLOWING FILES TO THE BUG REPORT:
Preprocessed source(s) and associated run script(s) are located at:
clang-cl: note: diagnostic msg: ../../tools/clang/crashreports\render_frame_impl-673e3a.cpp
clang-cl: note: diagnostic msg: ../../tools/clang/crashreports\render_frame_impl-673e3a.sh
clang-cl: note: diagnostic msg:

********************
`)
	llvmErrorMsg, ok := extractLLVMError(msg)
	if got, want := llvmErrorMsg, []byte(`LLVM ERROR: out of memory`); !ok || !bytes.Equal(got, want) {
		t.Errorf("extractLLVMError(msg)=%q, %t; want=%q; true",
			string(got), ok, string(want))
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

type fakeGomaInput struct {
	digests map[*gomapb.ExecReq_Input]digest.Data
	hashes  map[*gomapb.FileBlob]string
}

func (f *fakeGomaInput) setInputs(inputs []*gomapb.ExecReq_Input) {
	if f.digests == nil {
		f.digests = make(map[*gomapb.ExecReq_Input]digest.Data)
	}
	if f.hashes == nil {
		f.hashes = make(map[*gomapb.FileBlob]string)
	}
	for _, input := range inputs {
		f.digests[input] = digest.Bytes(input.GetFilename(), input.Content.Content)
		f.hashes[input.Content] = input.GetHashKey()
	}
}

func (f *fakeGomaInput) toDigest(ctx context.Context, in *gomapb.ExecReq_Input) (digest.Data, error) {
	d, ok := f.digests[in]
	if !ok {
		return nil, errors.New("not found")
	}
	return d, nil
}

func (f *fakeGomaInput) upload(ctx context.Context, blob *gomapb.FileBlob) (string, error) {
	h, ok := f.hashes[blob]
	if !ok {
		return "", errors.New("upload error")
	}
	return h, nil
}

type nopLogger struct{}

func (nopLogger) Debug(args ...interface{})                {}
func (nopLogger) Debugf(format string, arg ...interface{}) {}
func (nopLogger) Info(args ...interface{})                 {}
func (nopLogger) Infof(format string, arg ...interface{})  {}
func (nopLogger) Warn(args ...interface{})                 {}
func (nopLogger) Warnf(format string, arg ...interface{})  {}
func (nopLogger) Error(args ...interface{})                {}
func (nopLogger) Errorf(format string, arg ...interface{}) {}
func (nopLogger) Fatal(args ...interface{})                {}
func (nopLogger) Fatalf(format string, arg ...interface{}) {}
func (nopLogger) Sync() error                              { return nil }

func BenchmarkInputFiles(b *testing.B) {
	var inputs []*gomapb.ExecReq_Input
	for i := 0; i < 1000; i++ {
		content := fmt.Sprintf("content %d", i)
		blob := &gomapb.FileBlob{
			BlobType: gomapb.FileBlob_FILE.Enum(),
			Content:  []byte(content),
			FileSize: proto.Int64(int64(len(content))),
		}
		hashkey, err := hash.SHA256Proto(blob)
		if err != nil {
			b.Fatal(err)
		}
		inputs = append(inputs, &gomapb.ExecReq_Input{
			HashKey:  proto.String(hashkey),
			Filename: proto.String(fmt.Sprintf("input_%d", i)),
			Content:  blob,
		})
	}
	gi := &fakeGomaInput{}
	gi.setInputs(inputs)
	rootRel := func(filename string) (string, error) { return filename, nil }
	executableInputs := map[string]bool{}
	sema := make(chan struct{}, 5)
	ctx := log.NewContext(context.Background(), nopLogger{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inputFiles(ctx, inputs, gi, rootRel, executableInputs, sema)
	}
}
