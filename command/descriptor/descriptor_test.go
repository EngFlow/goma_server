// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package descriptor

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pb "go.chromium.org/goma/server/proto/command"
)

func TestVersion(t *testing.T) {
	for _, tc := range []struct {
		dout, vout string
		want       string
	}{
		{
			dout: "4.8\n",
			vout: `gcc (Ubuntu 4.8.4-2ubuntu1~14.04.3) 4.8.4
Copyright (C) 2013 Free Software Foundation, Inc.
This is free software; see the source for copying conditions.  There is NO
warranty; not even for MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.

`,
			want: "4.8[(Ubuntu 4.8.4-2ubuntu1~14.04.3) 4.8.4]",
		},
		{
			dout: "4.2.1\n",
			vout: `clang version 5.0.0 (trunk 305462)
Target: x86_64-unknown-linux-gnu
Thread model: posix
InstalledDir: /home/goma/src/chromium-git/src/./third_party/llvm-build/Release+Asserts/bin

  Registered Targets:
    aarch64    - AArch64 (little endian)
    aarch64_be - AArch64 (big endian)
    amdgcn     - AMD GCN GPUs
    arm        - ARM
    arm64      - ARM64 (little endian)
    armeb      - ARM (big endian)
    bpf        - BPF (host endian)
    bpfeb      - BPF (big endian)
    bpfel      - BPF (little endian)
    hexagon    - Hexagon
    lanai      - Lanai
    mips       - Mips
    mips64     - Mips64 [experimental]
    mips64el   - Mips64el [experimental]
    mipsel     - Mipsel
    msp430     - MSP430 [experimental]
    nvptx      - NVIDIA PTX 32-bit
    nvptx64    - NVIDIA PTX 64-bit
    ppc32      - PowerPC 32
    ppc64      - PowerPC 64
    ppc64le    - PowerPC 64 LE
    r600       - AMD GPUs HD2XXX-HD6XXX
    riscv32    - 32-bit RISC-V
    riscv64    - 64-bit RISC-V
    sparc      - Sparc
    sparcel    - Sparc LE
    sparcv9    - Sparc V9
    systemz    - SystemZ
    thumb      - Thumb
    thumbeb    - Thumb (big endian)
    x86        - 32-bit X86: Pentium-Pro and above
    x86-64     - 64-bit X86: EM64T and AMD64
    xcore      - XCore

`,
			want: "4.2.1[clang version 5.0.0 (trunk 305462)]",
		},
		{
			dout: "4.2.1\n",
			vout: `clang version 3.7.0 (https://chromium.googlesource.com/a/native_client/pnacl-clang.git e34aaa1fd835c13131bf82b16b768253e14e9247) (https://chromium.googlesource.com/a/native_client/pnacl-llvm.git 63a5544b9bf92b8ed6af03eae595947de1761fef) nacl-version=efa3f5d8ef135ed2463a75ac4630d1c448021400
Target: le32-unknown-nacl
Thread model: posix

`,
			want: "4.2.1[clang version 3.7.0 (https://chromium.googlesource.com/a/native_client/pnacl-clang.git e34aaa1fd835c13131bf82b16b768253e14e9247) (https://chromium.googlesource.com/a/native_client/pnacl-llvm.git 63a5544b9bf92b8ed6af03eae595947de1761fef) nacl-version=efa3f5d8ef135ed2463a75ac4630d1c448021400]",
		},
	} {
		got := Version([]byte(tc.dout), []byte(tc.vout))
		if got != tc.want {
			t.Errorf("Version(%q, %q)=%q; want=%q", tc.dout, tc.vout, got, tc.want)
		}
	}
}

func TestTarget(t *testing.T) {
	for _, tc := range []struct {
		out  string
		want string
	}{
		{
			out:  "x86_64-linux-gnu\n",
			want: "x86_64-linux-gnu",
		},
		{
			out:  "x86_64-unknown-linux-gnu\n",
			want: "x86_64-unknown-linux-gnu",
		},
		{
			out:  "le32-unknown-nacl\n",
			want: "le32-unknown-nacl",
		},
	} {
		got := Target([]byte(tc.out))
		if got != tc.want {
			t.Errorf("Target(%q)=%q; want=%q", tc.out, got, tc.want)
		}
	}
}

func TestJavaVersion(t *testing.T) {
	for _, tc := range []struct {
		out       string
		want      string
		wantError bool
	}{
		{
			out:       "not javac",
			wantError: true,
		},
		{
			out:  "javac 1.8.0_45-internal\n",
			want: "1.8.0_45-internal",
		},
	} {
		got, err := JavaVersion([]byte(tc.out))
		if tc.wantError && err == nil {
			t.Errorf("JavaVersion(%q)=_,nil; want error", tc.out)
		}
		if !tc.wantError && err != nil {
			t.Errorf("JavaVersion(%q)=_,%v; want nil", tc.out, err)
		}
		if got != tc.want {
			t.Errorf("JavaVersion(%q)=%q; want=%q", tc.out, got, tc.want)
		}
	}
}

func TestClexeVersion(t *testing.T) {
	// success case
	for _, tc := range []struct {
		out  string
		want string
	}{
		{
			out: `Microsoft (R) C/C++ Optimizing Compiler Version 19.11.25505 for x64
Copyright (C) Microsoft Corporation.  All rights reserved.`,
			want: "19.11.25505",
		},
		{
			out: `Microsoft (R) C/C++ Optimizing Compiler Version 19.00.24215.1 for x64
Copyright (C) Microsoft Corporation.  All rights reserved.`,
			want: "19.00.24215.1",
		},
	} {
		got, err := ClexeVersion([]byte(tc.out))
		if err != nil {
			t.Errorf("ClexeVersion(%q)=_,%v; want nil", tc.out, err)
		}
		if got != tc.want {
			t.Errorf("ClexeVersion(%q)=%q; want=%q", tc.out, got, tc.want)
		}
	}

	// failure case
	for _, tc := range []string{
		"",
		"Copyright (C) Microsoft Corporation.  All rights reserved.",
		"Microsoft (R) C/C++ Optimizing Compiler Version 19x00x24215x1 for x64",
		`clang version 3.7.0 (https://chromium.googlesource.com/a/native_client/pnacl-clang.git e34aaa1fd835c13131bf82b16b768253e14e9247) (https://chromium.googlesource.com/a/native_client/pnacl-llvm.git 63a5544b9bf92b8ed6af03eae595947de1761fef) nacl-version=efa3f5d8ef135ed2463a75ac4630d1c448021400
Target: le32-unknown-nacl
Thread model: posix`,
	} {
		_, err := ClexeVersion([]byte(tc))
		if err == nil {
			t.Errorf("ClexeVersion(%q)=_,nil; want error", tc)
		}
	}
}

func TestClexeTarget(t *testing.T) {
	// success case
	for _, tc := range []struct {
		out  string
		want string
	}{
		{
			out: `Microsoft (R) C/C++ Optimizing Compiler Version 19.11.25505 for x64
Copyright (C) Microsoft Corporation.  All rights reserved.`,
			want: "x64",
		},
		{
			out: `Microsoft (R) C/C++ Optimizing Compiler Version 19.11.25505 for x86
Copyright (C) Microsoft Corporation.  All rights reserved.`,
			want: "x86",
		},
		{
			out: `Microsoft (R) C/C++ Optimizing Compiler Version 19.00.24215.1 for x64
Copyright (C) Microsoft Corporation.  All rights reserved.`,
			want: "x64",
		},
	} {
		got, err := ClexeTarget([]byte(tc.out))
		if err != nil {
			t.Errorf("ClexeTarget(%q)=_,%v; want nil", tc.out, err)
		}
		if got != tc.want {
			t.Errorf("ClexeTarget(%q)=%q; want=%q", tc.out, got, tc.want)
		}
	}

	// failure case
	for _, tc := range []string{
		"",
		"Copyright (C) Microsoft Corporation.  All rights reserved.",
		"Microsoft (R) C/C++ Optimizing Compiler Version 19x00x24215x1 for x64",
		`clang version 3.7.0 (https://chromium.googlesource.com/a/native_client/pnacl-clang.git e34aaa1fd835c13131bf82b16b768253e14e9247) (https://chromium.googlesource.com/a/native_client/pnacl-llvm.git 63a5544b9bf92b8ed6af03eae595947de1761fef) nacl-version=efa3f5d8ef135ed2463a75ac4630d1c448021400
Target: le32-unknown-nacl
Thread model: posix`,
	} {
		_, err := ClexeTarget([]byte(tc))
		if err == nil {
			t.Errorf("ClexeTarget(%q)=_,nil; want error", tc)
		}
	}
}

func TestClangClVersion(t *testing.T) {
	// success case
	for _, tc := range []struct {
		out  string
		want string
	}{
		{
			out:  "clang version 6.0.0 (trunk 308728)\nTarget: x86_64-pc-windows-msvc\n",
			want: "clang version 6.0.0 (trunk 308728)",
		},
		{
			out:  "clang version 3.5.0 (trunk 225621)\r\nTarget: i686-pc-windows-msvc\r\n",
			want: "clang version 3.5.0 (trunk 225621)",
		},
		{
			out:  "clang version 3.5.0 (trunk 225621)\r\nTarget: i686-pc-windows-msvc",
			want: "clang version 3.5.0 (trunk 225621)",
		},
		{
			out: `clang version 9.0.0 (https://github.com/llvm/llvm-project/ 67510fac36d27b2e22c7cd955fc167136b737b93)
Target: x86_64-pc-windows-msvc
Thread model: posix
InstalledDir: /var/tmp/clang/third_party/llvm-build/Release+Asserts/bin
`,
			want: "clang version 9.0.0 (https://github.com/llvm/llvm-project/ 67510fac36d27b2e22c7cd955fc167136b737b93)",
		},
		{
			out:  "clang version 9.0.0 (https://github.com/llvm/llvm-project/ 67510fac36d27b2e22c7cd955fc167136b737b93)\r\nTarget: x86_64-pc-windows-msvc\r\nThread model: posix\r\nInstalledDir: /var/tmp/clang/third_party/llvm-build/Release+Asserts/bin\r\n",
			want: "clang version 9.0.0 (https://github.com/llvm/llvm-project/ 67510fac36d27b2e22c7cd955fc167136b737b93)",
		},
	} {
		got, err := ClangClVersion([]byte(tc.out))
		if err != nil {
			t.Errorf("ClangClVersion(%q)=_,%v; want nil", tc.out, err)
		}
		if got != tc.want {
			t.Errorf("ClangClVersion(%q)=%q; want=%q", tc.out, got, tc.want)
		}
	}

	// failure case
	for _, tc := range []string{
		"",
		"clang version",
		"Copyright (C) Microsoft Corporation.  All rights reserved.",
		"Microsoft (R) C/C++ Optimizing Compiler Version 19x00x24215x1 for x64",
	} {
		_, err := ClangClVersion([]byte(tc))
		if err == nil {
			t.Errorf("ClangClVersion(%q)=_,nil; want error", tc)
		}
	}
}

func TestClangClTarget(t *testing.T) {
	// success case
	for _, tc := range []struct {
		out  string
		want string
	}{
		{
			out:  "clang version 6.0.0 (trunk 308728)\nTarget: x86_64-pc-windows-msvc\n",
			want: "x86_64-pc-windows-msvc",
		},
		{
			out:  "clang version 3.5.0 (trunk 225621)\r\nTarget: i686-pc-windows-msvc\r\n",
			want: "i686-pc-windows-msvc",
		},
		{
			out: `clang version 9.0.0 (https://github.com/llvm/llvm-project/ 67510fac36d27b2e22c7cd955fc167136b737b93)
Target: x86_64-pc-windows-msvc
Thread model: posix
InstalledDir: /var/tmp/clang/third_party/llvm-build/Release+Asserts/bin
`,
			want: "x86_64-pc-windows-msvc",
		},
		{
			out:  "clang version 9.0.0 (https://github.com/llvm/llvm-project/ 67510fac36d27b2e22c7cd955fc167136b737b93)\r\nTarget: x86_64-pc-windows-msvc\r\nThread model: posix\r\nInstalledDir: /var/tmp/clang/third_party/llvm-build/Release+Asserts/bin\r\n",
			want: "x86_64-pc-windows-msvc",
		},
	} {
		got, err := ClangClTarget([]byte(tc.out))
		if err != nil {
			t.Errorf("ClangClTarget(%q)=_,%v; want nil", tc.out, err)
		}
		if got != tc.want {
			t.Errorf("ClangClTarget(%q)=%q; want=%q", tc.out, got, tc.want)
		}
	}

	// failure case
	for _, tc := range []string{
		"",
		"clang version",
		"clang version 3.5.0 (trunk 225621)\r\n",
		"Copyright (C) Microsoft Corporation.  All rights reserved.",
		"Microsoft (R) C/C++ Optimizing Compiler Version 19x00x24215x1 for x64",
	} {
		_, err := ClangClTarget([]byte(tc))
		if err == nil {
			t.Errorf("ClangClTarget(%q)=_,nil; want error", tc)
		}
	}
}

func TestResolveSymlinks(t *testing.T) {
	d := &Descriptor{
		fname: "dummy",
		CmdDescriptor: &pb.CmdDescriptor{
			Setup: &pb.CmdDescriptor_Setup{},
		},
		ToClientPath: func(p string) (string, error) { return p, nil },
		PathType:     pb.CmdDescriptor_POSIX,
		cwd:          "/dummy",
		cmddir:       "/dummy",
		seen:         make(map[string]bool),
	}
	dir, err := ioutil.TempDir("", "resolvesymlink")
	if err != nil {
		t.Fatalf("failed to create tmpdir: %v", err)
	}
	defer os.RemoveAll(dir)

	// clang++ -> clang -> clang-6.0
	err = os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	if err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}
	err = ioutil.WriteFile(filepath.Join(dir, "bin", "clang-6.0"), []byte("dummy"), 0644)
	if err != nil {
		t.Fatalf("failed to create clang-6.0: %v", err)
	}
	err = os.Symlink("clang-6.0", filepath.Join(dir, "bin", "clang"))
	if err != nil {
		t.Fatalf("failed to create clang: %v", err)
	}
	err = os.Symlink("clang", filepath.Join(dir, "bin", "clang++"))
	if err != nil {
		t.Fatalf("failed to create clang++: %v", err)
	}

	fs, err := newFilespec(filepath.Join(dir, "bin", "clang++"))
	if err != nil {
		t.Fatalf("filespec(%s)=,%v; want nil error", filepath.Join(dir, "bin", "clang++"), err)
	}
	err = d.resolveSymlinks("", fs)
	if err != nil {
		t.Errorf("resolveSymlinks(%s, %v)=_,%v; want nil error", "", fs, err)
	}
	var expectedFiles []*pb.FileSpec
	for _, f := range []string{"clang", "clang-6.0"} {
		fsc, err := newFilespec(filepath.Join(dir, "bin", f))
		if err != nil {
			t.Fatalf("filespec(%s)=_,%v; want nil error", f, err)
		}
		cfsc, err := d.filespecProto(fsc)
		if err != nil {
			t.Fatalf("filespecProto(%v)=_, %v; want nil error", fsc, err)
		}
		expectedFiles = append(expectedFiles, cfsc)
	}
	if !reflect.DeepEqual(d.Setup.Files, expectedFiles) {
		t.Errorf("d.Setup.Files=%q; want %q", d.Setup.Files, expectedFiles)
	}
}
