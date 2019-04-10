// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package descriptor

import (
	"reflect"
	"testing"

	pb "go.chromium.org/goma/server/proto/command"
)

func TestRelocateCmd(t *testing.T) {
	// Real example from chromium linux.
	cs := &pb.CmdDescriptor_Setup{
		CmdFile: &pb.FileSpec{
			Path:         "bin/clang",
			Size:         86885440,
			Hash:         "daacc7bcef72015968bfdda381b8ac27fe735d125314ed17d8320dbda89d7be3",
			IsExecutable: true,
		},
		CmdDir: "/var/opt/chromium_clang_linux/third_party/llvm-build/Release+Asserts",
		Files: []*pb.FileSpec{
			&pb.FileSpec{
				Path:         "/usr/bin/as",
				Size:         356952,
				Hash:         "ffc7f9fee274e30e308577761adc5fe97b2b22403874500a4c9f26517755ec33",
				IsExecutable: true,
			},
			&pb.FileSpec{
				Path:         "/usr/bin/objcopy",
				Size:         220128,
				Hash:         "e6ddd34643b436bfe1754f2719ac8ec17ff15497091db592e1b4e335f3653291",
				IsExecutable: true,
			},
			&pb.FileSpec{
				Path:    "/usr/bin/ld",
				Symlink: "ld.bfd",
			},
			&pb.FileSpec{
				Path:         "/usr/bin/ld.bfd",
				Size:         1050912,
				Hash:         "850873e6c4def272aea40f906df43bcb5ad6a7343593dde2dcb89e1a0e4cd6f4",
				IsExecutable: true,
			},
			&pb.FileSpec{
				Path:         "/usr/bin/nm",
				Size:         40704,
				Hash:         "9119a26b9dffd94cf2f87f10ff0baef81f0912a84a920f5f3441b3cfaf59ff2b",
				IsExecutable: true,
			},
			&pb.FileSpec{
				Path:         "/usr/bin/strip",
				Size:         220128,
				Hash:         "86aa93fd3010abe601c179a4600acbd348b13d76c66f861591c4d9c615687264",
				IsExecutable: true,
			},
		},
		PathType: pb.CmdDescriptor_POSIX,
	}

	subprogSetups := map[string]*pb.CmdDescriptor_Setup{
		"/home/goma/work/chrome/src/out/Release/../../third_party/llvm-build/Release+Asserts/lib/libFindBadConstructs.so": &pb.CmdDescriptor_Setup{
			CmdFile: &pb.FileSpec{
				Path:         "lib/libFindBadConstructs.so",
				Size:         521748,
				Hash:         "1c5e11d7975e49ab4396570f54f13d71685005a18593be29bf462b51f45954f6",
				IsExecutable: true,
			},
			CmdDir:   "/var/opt/chromium_clang_linux/third_party/llvm-build/Release+Asserts",
			PathType: pb.CmdDescriptor_POSIX,
		},
	}

	const reqCmdPath = "/home/goma/work/chrome/src/third_party/llvm-build/Release+Asserts/bin/clang++"

	cmdFiles, err := RelocateCmd(reqCmdPath, cs, subprogSetups)
	if err != nil {
		t.Errorf("RelocateCmd(%q, %q, %q)=_, _, %v; want nil-error", reqCmdPath, cs, subprogSetups, err)
	}

	want := []*pb.FileSpec{
		&pb.FileSpec{
			Path:         "/home/goma/work/chrome/src/third_party/llvm-build/Release+Asserts/bin/clang++",
			Size:         86885440,
			Hash:         "daacc7bcef72015968bfdda381b8ac27fe735d125314ed17d8320dbda89d7be3",
			IsExecutable: true,
		},
		&pb.FileSpec{
			Path:         "/usr/bin/as",
			Size:         356952,
			Hash:         "ffc7f9fee274e30e308577761adc5fe97b2b22403874500a4c9f26517755ec33",
			IsExecutable: true,
		},
		&pb.FileSpec{
			Path:         "/usr/bin/objcopy",
			Size:         220128,
			Hash:         "e6ddd34643b436bfe1754f2719ac8ec17ff15497091db592e1b4e335f3653291",
			IsExecutable: true,
		},
		&pb.FileSpec{
			Path:    "/usr/bin/ld",
			Symlink: "ld.bfd",
		},
		&pb.FileSpec{
			Path:         "/usr/bin/ld.bfd",
			Size:         1050912,
			Hash:         "850873e6c4def272aea40f906df43bcb5ad6a7343593dde2dcb89e1a0e4cd6f4",
			IsExecutable: true,
		},
		&pb.FileSpec{
			Path:         "/usr/bin/nm",
			Size:         40704,
			Hash:         "9119a26b9dffd94cf2f87f10ff0baef81f0912a84a920f5f3441b3cfaf59ff2b",
			IsExecutable: true,
		},
		&pb.FileSpec{
			Path:         "/usr/bin/strip",
			Size:         220128,
			Hash:         "86aa93fd3010abe601c179a4600acbd348b13d76c66f861591c4d9c615687264",
			IsExecutable: true,
		},
		&pb.FileSpec{
			Path:         "/home/goma/work/chrome/src/out/Release/../../third_party/llvm-build/Release+Asserts/lib/libFindBadConstructs.so",
			Size:         521748,
			Hash:         "1c5e11d7975e49ab4396570f54f13d71685005a18593be29bf462b51f45954f6",
			IsExecutable: true,
		},
	}

	if !reflect.DeepEqual(cmdFiles, want) {
		t.Errorf("RelocateCmd(%q, %q, %q)=%v; want=%v", reqCmdPath, cs, subprogSetups, cmdFiles, want)
	}
}

func TestRelocateCmdSubprog(t *testing.T) {
	// The main binary is not relocatable, but subprogram is relocatable.
	cs := &pb.CmdDescriptor_Setup{
		CmdFile: &pb.FileSpec{
			Path:         "/usr/bin/clang",
			Size:         1,
			Hash:         "a",
			IsExecutable: true,
		},
		CmdDir:   "/tmp",
		PathType: pb.CmdDescriptor_POSIX,
	}

	// objdump is relocatable, and it's requested to place at /opt/bin/objdump.
	subprogSetups := map[string]*pb.CmdDescriptor_Setup{
		"/opt/bin/objdump": &pb.CmdDescriptor_Setup{
			CmdFile: &pb.FileSpec{
				Path:         "bin/objdump",
				Size:         2,
				Hash:         "b",
				IsExecutable: true,
			},
			CmdDir:   "/cmddir",
			PathType: pb.CmdDescriptor_POSIX,
		},
	}

	const reqCmdPath = "/usr/bin/clang"

	cmdFiles, err := RelocateCmd(reqCmdPath, cs, subprogSetups)
	if err != nil {
		t.Errorf("RelocateCmd(%q, %q, %q)=_, _, %v; want nil-error", reqCmdPath, cs, subprogSetups, err)
	}

	// objdump must be included in FileSpec. clang is not relocatable, so it will be included as is.
	got := cmdFiles
	want := []*pb.FileSpec{
		&pb.FileSpec{
			Path:         "/usr/bin/clang",
			Size:         1,
			Hash:         "a",
			IsExecutable: true,
		},
		&pb.FileSpec{
			Path:         "/opt/bin/objdump",
			Size:         2,
			Hash:         "b",
			IsExecutable: true,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("RelocateCmd(%q, %q, %q)=_, %v, nil; want=_, %v, nil", reqCmdPath, cs, subprogSetups, got, want)
	}
}
