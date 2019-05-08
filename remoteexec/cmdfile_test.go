// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"context"
	"io"
	"reflect"
	"testing"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	pb "go.chromium.org/goma/server/proto/command"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/remoteexec/merkletree"
)

type dummyCmdStorage struct {
}

func (dcs dummyCmdStorage) Open(ctx context.Context, hash string) (io.ReadCloser, error) {
	return nil, nil
}

func TestFileSpecToEntry(t *testing.T) {
	tests := []struct {
		input     pb.FileSpec
		wantEntry merkletree.Entry
		wantErr   bool
	}{
		// Executable files.
		{
			pb.FileSpec{
				Path:         "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/pnacl-clang++",
				Hash:         "7bf4c008d0321a9956279edd58fd2078569e9595fbfe5775c228836b36d71796",
				Size:         1234,
				IsExecutable: true,
			},
			merkletree.Entry{
				Name:         "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/pnacl-clang++",
				IsExecutable: true,
				Data: digest.New(
					cmdFileObj{
						hash:    "7bf4c008d0321a9956279edd58fd2078569e9595fbfe5775c228836b36d71796",
						storage: dummyCmdStorage{},
					},
					&rpb.Digest{
						Hash:      "7bf4c008d0321a9956279edd58fd2078569e9595fbfe5775c228836b36d71796",
						SizeBytes: 1234,
					}),
			}, false,
		}, {
			pb.FileSpec{
				Path:         "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/clang",
				Hash:         "42151bf3845e7b44d0339964cdc19ce55427e7eee9e4bf0d306fe313ed8b5db8",
				Size:         4567,
				IsExecutable: true,
			},
			merkletree.Entry{
				Name:         "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/clang",
				IsExecutable: true,
				Data: digest.New(
					cmdFileObj{
						hash:    "42151bf3845e7b44d0339964cdc19ce55427e7eee9e4bf0d306fe313ed8b5db8",
						storage: dummyCmdStorage{},
					},
					&rpb.Digest{
						Hash:      "42151bf3845e7b44d0339964cdc19ce55427e7eee9e4bf0d306fe313ed8b5db8",
						SizeBytes: 4567,
					}),
			}, false,
		}, {
			// Test IsExecutable=false.
			pb.FileSpec{
				Path:         "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/../lib/libc++.so.1.0",
				Hash:         "7fc88a31bbededbe1f276c23a66797b21cf8d7f837e6580e70b2755a817a08c7",
				Size:         1111,
				IsExecutable: false,
			},
			merkletree.Entry{
				Name:         "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/../lib/libc++.so.1.0",
				IsExecutable: false,
				Data: digest.New(
					cmdFileObj{
						hash:    "7fc88a31bbededbe1f276c23a66797b21cf8d7f837e6580e70b2755a817a08c7",
						storage: dummyCmdStorage{},
					},
					&rpb.Digest{
						Hash:      "7fc88a31bbededbe1f276c23a66797b21cf8d7f837e6580e70b2755a817a08c7",
						SizeBytes: 1111,
					}),
			}, false,
		}, {
			// Invalid entry.
			pb.FileSpec{
				Path:    "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/../lib/libc++.so.1.0",
				Hash:    "7fc88a31bbededbe1f276c23a66797b21cf8d7f837e6580e70b2755a817a08c7",
				Symlink: "libc++.so.1.0",
			},
			merkletree.Entry{}, true,
		}, {
			// Dir.
			pb.FileSpec{
				Path: "../../native_client/toolchain/linux_x86/pnacl_newlib/bin",
			},
			merkletree.Entry{
				Name: "../../native_client/toolchain/linux_x86/pnacl_newlib/bin",
			}, false,
		}, {
			// Symlinks.
			pb.FileSpec{
				Path:    "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/clang++",
				Symlink: "clang",
			},
			merkletree.Entry{
				Name:   "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/clang++",
				Target: "clang",
			}, false,
		}, {
			pb.FileSpec{
				Path:    "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/../lib/libc++.so",
				Symlink: "libc++.so.1",
			},
			merkletree.Entry{
				Name:   "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/../lib/libc++.so",
				Target: "libc++.so.1",
			}, false,
		}, {
			pb.FileSpec{
				Path:    "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/clang++",
				Symlink: "libc++.so.1.0",
			},
			merkletree.Entry{
				Name:   "../../native_client/toolchain/linux_x86/pnacl_newlib/bin/clang++",
				Target: "libc++.so.1.0",
			}, false,
		},
	}

	for _, test := range tests {
		entry, err := fileSpecToEntry(context.Background(), &test.input, dummyCmdStorage{})
		if !reflect.DeepEqual(entry, test.wantEntry) || test.wantErr != (err != nil) {
			t.Errorf("fileSpecToEntry(ctx, %v, dummyCmdStorage)=%v, %v, want=%v, wantErr=%v", test.input,
				entry, err, test.wantEntry, test.wantErr)
		}
	}
}
