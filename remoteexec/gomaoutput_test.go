// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"context"
	"path"
	"testing"

	rpb "go.chromium.org/goma/server/proto/remote-apis/build/bazel/remote/execution/v2"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
	gomapb "go.chromium.org/goma/server/proto/api"
	"go.chromium.org/goma/server/remoteexec/digest"
)

func TestOutputDirectory(t *testing.T) {
	ctx := context.Background()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	f1 := digest.Bytes("file1", []byte("file1"))
	f2 := digest.Bytes("file2", []byte("file2"))
	f3 := digest.Bytes("file3", []byte("file3"))
	cluster.rbe.cas.Set(f1)
	cluster.rbe.cas.Set(f2)
	cluster.rbe.cas.Set(f3)
	dir2 := &rpb.Directory{
		Files: []*rpb.FileNode{
			{
				Name:   "file3",
				Digest: f3.Digest(),
			},
		},
	}
	dir2data, err := digest.Proto(dir2)
	if err != nil {
		t.Fatalf("dir2: %v", err)
	}
	dir1 := &rpb.Directory{
		Files: []*rpb.FileNode{
			{
				Name:   "file2",
				Digest: f2.Digest(),
			},
		},
		Directories: []*rpb.DirectoryNode{
			{
				Name:   "dir2",
				Digest: dir2data.Digest(),
			},
		},
	}
	dir1data, err := digest.Proto(dir1)
	if err != nil {
		t.Fatalf("dir1: %v", err)
	}

	td, err := cluster.rbe.setProto(ctx, &rpb.Tree{
		Root: &rpb.Directory{
			Files: []*rpb.FileNode{
				{
					Name:   "file1",
					Digest: f1.Digest(),
				},
			},
			Directories: []*rpb.DirectoryNode{
				{
					Name:   "dir1",
					Digest: dir1data.Digest(),
				},
			},
		},
		Children: []*rpb.Directory{
			dir1,
			dir2,
		},
	})
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	var filepath posixpath.FilePath

	gout := gomaOutput{
		gomaResp: &gomapb.ExecResp{
			Result: &gomapb.ExecResult{},
		},
		bs:       cluster.adapter.Client,
		instance: path.Join(cluster.rbe.instancePrefix, "default_instance"),
		gomaFile: cluster.adapter.GomaFile,
	}

	gout.outputDirectory(ctx, filepath, "out", &rpb.OutputDirectory{
		Path:       "out",
		TreeDigest: td,
	})

	if len(gout.gomaResp.ErrorMessage) > 0 {
		t.Errorf("resp errorMessage %q; want no error", gout.gomaResp.ErrorMessage)
	}
	want := []*gomapb.ExecResult_Output{
		{
			Filename: proto.String("out/file1"),
			Blob: &gomapb.FileBlob{
				BlobType: gomapb.FileBlob_FILE.Enum(),
				Content:  []byte("file1"),
				FileSize: proto.Int64(int64(len("file1"))),
			},
			IsExecutable: proto.Bool(false),
		},
		{
			Filename: proto.String("out/dir1/file2"),
			Blob: &gomapb.FileBlob{
				BlobType: gomapb.FileBlob_FILE.Enum(),
				Content:  []byte("file2"),
				FileSize: proto.Int64(int64(len("file2"))),
			},
			IsExecutable: proto.Bool(false),
		},
		{
			Filename: proto.String("out/dir1/dir2/file3"),
			Blob: &gomapb.FileBlob{
				BlobType: gomapb.FileBlob_FILE.Enum(),
				Content:  []byte("file3"),
				FileSize: proto.Int64(int64(len("file3"))),
			},
			IsExecutable: proto.Bool(false),
		},
	}
	if diff := cmp.Diff(want, gout.gomaResp.Result.Output, cmp.Comparer(proto.Equal)); diff != "" {
		t.Errorf("output diff -want +got:\n%s", diff)
	}
}
