// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"context"
	"fmt"
	"path"
	"strings"
	"testing"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
	gomapb "go.chromium.org/goma/server/proto/api"
	"go.chromium.org/goma/server/remoteexec/digest"
)

type goutTestFile struct {
	name string
	data digest.Data
	node *rpb.FileNode
}

func makeFileNode(name string) *goutTestFile {
	// Contents are the same as the filename.
	d := digest.Bytes(name, []byte(name))
	return &goutTestFile{
		name: name,
		data: d,
		node: &rpb.FileNode{
			Name:   name,
			Digest: d.Digest(),
		},
	}
}

func makeDirNode(t *testing.T, name string, dir *rpb.Directory) *rpb.DirectoryNode {
	dirdata, err := digest.Proto(dir)
	if err != nil {
		t.Fatalf("dir %s: %v", name, err)
	}
	return &rpb.DirectoryNode{
		Name:   name,
		Digest: dirdata.Digest(),
	}
}

func makeFileBlob(contents string) *gomapb.FileBlob {
	return &gomapb.FileBlob{
		BlobType: gomapb.FileBlob_FILE.Enum(),
		Content:  []byte(contents),
		FileSize: proto.Int64(int64(len(contents))),
	}
}

func TestToFileBlob(t *testing.T) {
	ctx := context.Background()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	f1 := makeFileNode("file1")
	f2 := makeFileNode("file2")
	f3 := makeFileNode("file3")
	cluster.rbe.cas.Set(f1.data)
	cluster.rbe.cas.Set(f2.data)
	cluster.rbe.cas.Set(f3.data)

	gout := gomaOutput{
		bs:       cluster.adapter.Client,
		instance: path.Join(cluster.rbe.instancePrefix, "default_instance"),
		gomaFile: cluster.adapter.GomaFile,
	}

	var blobs []*gomapb.FileBlob
	for _, f := range []*goutTestFile{f1, f2, f3} {
		blob, err := gout.toFileBlob(ctx, &rpb.OutputFile{
			Path:   f.name,
			Digest: f.node.Digest,
		})
		if err != nil {
			t.Errorf("toFileBlob returned err: %v", err)
			continue
		}
		blobs = append(blobs, blob)
	}

	var want []*gomapb.FileBlob
	for _, f := range []*goutTestFile{f1, f2, f3} {
		want = append(want, makeFileBlob(f.name))
	}
	if diff := cmp.Diff(want, blobs, cmp.Comparer(proto.Equal)); diff != "" {
		t.Errorf("output diff -want +got:\n%s", diff)
	}
}

func TestToFileBlobLarge(t *testing.T) {
	ctx := context.Background()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	gout := gomaOutput{
		bs:       cluster.adapter.Client,
		instance: path.Join(cluster.rbe.instancePrefix, "default_instance"),
		gomaFile: cluster.adapter.GomaFile,
	}

	mibSize := int64(1024 * 1024)
	bigData := digest.Bytes("fbig", make([]byte, 5*mibSize))
	fBig := &goutTestFile{
		name: "fbig",
		data: bigData,
		node: &rpb.FileNode{
			Name:   "fbig",
			Digest: bigData.Digest(),
		},
	}
	cluster.rbe.cas.Set(fBig.data)

	blob, err := gout.toFileBlob(ctx, &rpb.OutputFile{
		Path:   fBig.name,
		Digest: fBig.node.Digest,
	})
	if err != nil {
		t.Errorf("toFileBlob returned err: %v", err)
	}

	var chunkBlobs []*gomapb.FileBlob
	for i, hk := range blob.HashKey {
		// Look up one blob at a time to avoid exceeding the 4MiB gRPC response size limit.
		resp, err := gout.gomaFile.LookupFile(ctx, &gomapb.LookupFileReq{
			HashKey: []string{hk},
		})
		if err != nil {
			t.Errorf("gomaFile.LookupFile(HashKey[%d]=%s) returned err: %v", i, hk, err)
			continue
		}
		if len(resp.Blob) != 1 {
			t.Errorf("gomaFile.LookupFile(HashKey[%d]=%s) returned %d blobs, expected 1", i, hk, len(resp.Blob))
			continue
		}
		chunkBlobs = append(chunkBlobs, resp.Blob[0])
	}

	wantBlob := &gomapb.FileBlob{
		BlobType: gomapb.FileBlob_FILE_META.Enum(),
		FileSize: proto.Int64(5 * mibSize),
		HashKey:  blob.HashKey, // These don't need to be checked.
	}
	if diff := cmp.Diff(wantBlob, blob, cmp.Comparer(proto.Equal)); diff != "" {
		t.Errorf("output diff -want +got:\n%s", diff)
	}

	wantChunkBlobs := []*gomapb.FileBlob{
		{
			BlobType: gomapb.FileBlob_FILE_CHUNK.Enum(),
			Offset:   proto.Int64(0),
			Content:  make([]byte, 2*mibSize),
			FileSize: proto.Int64(5 * mibSize),
		}, {
			BlobType: gomapb.FileBlob_FILE_CHUNK.Enum(),
			Offset:   proto.Int64(2 * mibSize),
			Content:  make([]byte, 2*mibSize),
			FileSize: proto.Int64(5 * mibSize),
		}, {
			BlobType: gomapb.FileBlob_FILE_CHUNK.Enum(),
			Offset:   proto.Int64(4 * mibSize),
			Content:  make([]byte, mibSize),
			FileSize: proto.Int64(5 * mibSize),
		},
	}
	if diff := cmp.Diff(wantChunkBlobs, chunkBlobs, cmp.Comparer(proto.Equal)); diff != "" {
		t.Errorf("output diff -want +got:\n%s", diff)
	}
}

func TestOutputFile(t *testing.T) {
	ctx := context.Background()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	f1 := makeFileNode("file1")
	f2 := makeFileNode("file2")
	f3 := makeFileNode("file3")
	cluster.rbe.cas.Set(f1.data)
	cluster.rbe.cas.Set(f2.data)
	cluster.rbe.cas.Set(f3.data)

	gout := gomaOutput{
		gomaResp: &gomapb.ExecResp{
			Result: &gomapb.ExecResult{},
		},
		bs:       cluster.adapter.Client,
		instance: path.Join(cluster.rbe.instancePrefix, "default_instance"),
		gomaFile: cluster.adapter.GomaFile,
	}

	for _, f := range []*goutTestFile{f1, f2, f3} {
		gout.outputFile(ctx, f.name, &rpb.OutputFile{
			Path:   f.name,
			Digest: f.node.Digest,
		})
	}

	if len(gout.gomaResp.ErrorMessage) > 0 {
		t.Errorf("resp errorMessage %q; want no error", gout.gomaResp.ErrorMessage)
	}
	var want []*gomapb.ExecResult_Output
	for _, f := range []*goutTestFile{f1, f2, f3} {
		want = append(want, &gomapb.ExecResult_Output{
			Filename:     proto.String(f.name),
			Blob:         makeFileBlob(f.name),
			IsExecutable: proto.Bool(false),
		})
	}
	if diff := cmp.Diff(want, gout.gomaResp.Result.Output, cmp.Comparer(proto.Equal)); diff != "" {
		t.Errorf("output diff -want +got:\n%s", diff)
	}
}

func TestOutputFilesConcurrent(t *testing.T) {
	ctx := context.Background()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	var files []*goutTestFile
	for i := 0; i < 1000; i++ {
		f := makeFileNode(fmt.Sprintf("file%d", i))
		files = append(files, f)
		cluster.rbe.cas.Set(f.data)
	}

	gout := gomaOutput{
		gomaResp: &gomapb.ExecResp{
			Result: &gomapb.ExecResult{},
		},
		bs:       cluster.adapter.Client,
		instance: path.Join(cluster.rbe.instancePrefix, "default_instance"),
		gomaFile: cluster.adapter.GomaFile,
	}

	var outputFiles []*rpb.OutputFile
	for _, f := range files {
		outputFiles = append(outputFiles, &rpb.OutputFile{
			Path:   f.name,
			Digest: f.node.Digest,
		})
	}

	sema := make(chan struct{}, 20)
	gout.outputFilesConcurrent(ctx, outputFiles, sema)

	if len(gout.gomaResp.ErrorMessage) > 0 {
		t.Errorf("resp errorMessage %q; want no error", gout.gomaResp.ErrorMessage)
	}
	var want []*gomapb.ExecResult_Output
	for _, f := range files {
		want = append(want, &gomapb.ExecResult_Output{
			Filename:     proto.String(f.name),
			Blob:         makeFileBlob(f.name),
			IsExecutable: proto.Bool(false),
		})
	}
	if diff := cmp.Diff(want, gout.gomaResp.Result.Output, cmp.Comparer(proto.Equal)); diff != "" {
		t.Errorf("output diff -want +got:\n%s", diff)
	}
}

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

	f1 := makeFileNode("file1")
	f2 := makeFileNode("file2")
	f3 := makeFileNode("file3")
	cluster.rbe.cas.Set(f1.data)
	cluster.rbe.cas.Set(f2.data)
	cluster.rbe.cas.Set(f3.data)
	dir2 := &rpb.Directory{
		Files: []*rpb.FileNode{f3.node},
	}
	dir1 := &rpb.Directory{
		Files: []*rpb.FileNode{f2.node},
		Directories: []*rpb.DirectoryNode{
			makeDirNode(t, "dir2", dir2),
		},
	}

	td, err := cluster.rbe.setProto(ctx, &rpb.Tree{
		Root: &rpb.Directory{
			Files: []*rpb.FileNode{f1.node},
			Directories: []*rpb.DirectoryNode{
				makeDirNode(t, "dir1", dir1),
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

	sema := make(chan struct{}, 2)
	gout.outputDirectory(ctx, filepath, "out", &rpb.OutputDirectory{
		Path:       "out",
		TreeDigest: td,
	}, sema)

	if len(gout.gomaResp.ErrorMessage) > 0 {
		t.Errorf("resp errorMessage %q; want no error", gout.gomaResp.ErrorMessage)
	}
	want := []*gomapb.ExecResult_Output{
		{
			Filename:     proto.String("out/file1"),
			Blob:         makeFileBlob("file1"),
			IsExecutable: proto.Bool(false),
		},
		{
			Filename:     proto.String("out/dir1/file2"),
			Blob:         makeFileBlob("file2"),
			IsExecutable: proto.Bool(false),
		},
		{
			Filename:     proto.String("out/dir1/dir2/file3"),
			Blob:         makeFileBlob("file3"),
			IsExecutable: proto.Bool(false),
		},
	}
	if diff := cmp.Diff(want, gout.gomaResp.Result.Output, cmp.Comparer(proto.Equal)); diff != "" {
		t.Errorf("output diff -want +got:\n%s", diff)
	}
}

func TestTraverseTree(t *testing.T) {
	ctx := context.Background()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	f1 := makeFileNode("file1")
	f2 := makeFileNode("file2")
	f3 := makeFileNode("file3")
	cluster.rbe.cas.Set(f1.data)
	cluster.rbe.cas.Set(f2.data)
	cluster.rbe.cas.Set(f3.data)
	dir2 := &rpb.Directory{
		Files: []*rpb.FileNode{f3.node},
	}
	dir1 := &rpb.Directory{
		Files: []*rpb.FileNode{f2.node},
		Directories: []*rpb.DirectoryNode{
			makeDirNode(t, "dir2", dir2),
		},
	}
	tree := &rpb.Tree{
		Root: &rpb.Directory{
			Files: []*rpb.FileNode{f1.node},
			Directories: []*rpb.DirectoryNode{
				makeDirNode(t, "dir1", dir1),
			},
		},
		Children: []*rpb.Directory{
			dir1,
			dir2,
		},
	}

	ds := digest.NewStore()
	for _, c := range tree.Children {
		d, err := digest.Proto(c)
		if err != nil {
			t.Errorf("digest for children %s: %v", c, err)
			continue
		}
		ds.Set(d)
	}

	var filepath posixpath.FilePath

	result := traverseTree(ctx, filepath, "out", tree.Root, ds)

	want := []*rpb.OutputFile{
		{
			Path:   "out/file1",
			Digest: f1.node.Digest,
		}, {
			Path:   "out/dir1/file2",
			Digest: f2.node.Digest,
		}, {
			Path:   "out/dir1/dir2/file3",
			Digest: f3.node.Digest,
		},
	}
	if diff := cmp.Diff(want, result, cmp.Comparer(proto.Equal)); diff != "" {
		t.Errorf("output diff -want +got:\n%s", diff)
	}
}

func TestReduceRespSize(t *testing.T) {
	ctx := context.Background()

	contents := []string{
		strings.Repeat("###", 10),
		strings.Repeat("###", 20),
		strings.Repeat("###", 30),
		strings.Repeat("###", 40),
		strings.Repeat("###", 50),
		strings.Repeat("###", 60),
		strings.Repeat("###", 70),
		strings.Repeat("###", 80),
		strings.Repeat("###", 90),
		strings.Repeat("###", 100),
	}

	var outputs []*gomapb.ExecResult_Output
	for i, content := range contents {
		outputs = append(outputs, &gomapb.ExecResult_Output{
			Filename:     proto.String(fmt.Sprintf("file%d", i)),
			Blob:         makeFileBlob(content),
			IsExecutable: proto.Bool(false),
		})
	}
	convertToStored := func(in *gomapb.ExecResult_Output) *gomapb.ExecResult_Output {
		return &gomapb.ExecResult_Output{
			Filename: in.Filename,
			Blob: &gomapb.FileBlob{
				BlobType: gomapb.FileBlob_FILE_REF.Enum(),
				FileSize: proto.Int64(in.Blob.GetFileSize()),
				// Hashkey will be checked separately.
			},
			IsExecutable: in.IsExecutable,
		}
	}

	makeResp := func(outputs []*gomapb.ExecResult_Output) *gomapb.ExecResp {
		return &gomapb.ExecResp{
			Result: &gomapb.ExecResult{
				ExitStatus:   proto.Int32(1234),
				StdoutBuffer: []byte("Hello"),
				StderrBuffer: []byte("Goodbye"),
				Output:       outputs,
			},
		}
	}
	respSizeAllOutputs := proto.Size(makeResp(outputs))

	metaOutput := &gomapb.ExecResult_Output{
		Filename: proto.String("bigfile"),
		Blob: &gomapb.FileBlob{
			BlobType: gomapb.FileBlob_FILE_META.Enum(),
			FileSize: proto.Int64(5 * 1024 * 1024),
			HashKey:  []string{"0123", "4567", "89ab"},
		},
		IsExecutable: proto.Bool(false),
	}

	for _, tc := range []struct {
		desc       string
		output     []*gomapb.ExecResult_Output
		byteLimit  int
		wantOutput []*gomapb.ExecResult_Output
		wantErr    bool
	}{
		{
			desc:      "empty",
			output:    []*gomapb.ExecResult_Output{},
			byteLimit: 1,
			wantErr:   true,
		}, {
			desc:       "no change in size",
			output:     outputs,
			byteLimit:  respSizeAllOutputs,
			wantOutput: outputs,
		}, {
			desc:       "limit too high",
			output:     outputs,
			byteLimit:  respSizeAllOutputs * 2,
			wantOutput: outputs,
		}, {
			desc:      "limit too low",
			output:    outputs,
			byteLimit: 1,
			wantErr:   true,
		}, {
			desc: "blob smaller than hashkey",
			output: []*gomapb.ExecResult_Output{
				outputs[0],
				outputs[1], // The largest blob has size 3 * 20 = 60, < hashkey size=64
			},
			byteLimit: proto.Size(makeResp([]*gomapb.ExecResult_Output{
				outputs[0],
				outputs[1],
			})) - 1,
			wantErr: true,
		}, {
			desc:      "remove all blobs",
			byteLimit: respSizeAllOutputs - 600,
			output:    outputs,
			wantOutput: []*gomapb.ExecResult_Output{
				convertToStored(outputs[0]),
				convertToStored(outputs[1]),
				convertToStored(outputs[2]),
				convertToStored(outputs[3]),
				convertToStored(outputs[4]),
				convertToStored(outputs[5]),
				convertToStored(outputs[6]),
				convertToStored(outputs[7]),
				convertToStored(outputs[8]),
				convertToStored(outputs[9]),
			},
		}, {
			desc:      "skip non-FILE",
			byteLimit: respSizeAllOutputs - 600,
			output: []*gomapb.ExecResult_Output{
				outputs[0],
				outputs[1],
				outputs[2],
				outputs[3],
				outputs[4],
				metaOutput,
				outputs[5],
				outputs[6],
				outputs[7],
				outputs[8],
				outputs[9],
			},
			wantOutput: []*gomapb.ExecResult_Output{
				convertToStored(outputs[0]),
				convertToStored(outputs[1]),
				convertToStored(outputs[2]),
				convertToStored(outputs[3]),
				convertToStored(outputs[4]),
				metaOutput,
				convertToStored(outputs[5]),
				convertToStored(outputs[6]),
				convertToStored(outputs[7]),
				convertToStored(outputs[8]),
				convertToStored(outputs[9]),
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			cluster := &fakeCluster{}
			err := cluster.setup(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			defer cluster.teardown()

			gout := gomaOutput{
				gomaFile: cluster.adapter.GomaFile,
				// Avoid changing the original gomapb.ExecResult_Output values during test.
				gomaResp: proto.Clone(makeResp(tc.output)).(*gomapb.ExecResp),
			}
			sema := make(chan struct{}, 3)
			err = gout.reduceRespSize(ctx, tc.byteLimit, sema)
			result := gout.gomaResp.Result

			if err != nil && !tc.wantErr {
				t.Errorf("got err=%v, want nil", err)
				return
			}
			if tc.wantErr {
				if err == nil {
					t.Errorf("got err=nil, want !nil")
				}
				return
			}

			if len(result.Output) != len(tc.wantOutput) {
				t.Errorf("got len(result.Output)=%d, want %d", len(result.Output), len(tc.wantOutput))
				return
			}
			if proto.Size(result) > tc.byteLimit {
				t.Errorf("got size=%d, want<=%d", proto.Size(result), tc.byteLimit)
			}

			// Since we don't have expected hash keys, check them separately and remove them from the result.
			for i, resOut := range result.Output {
				if resOut.Blob.GetBlobType() != gomapb.FileBlob_FILE_REF {
					continue
				}
				hks := resOut.Blob.HashKey
				if len(hks) != 1 {
					t.Errorf("len(hks)=%d, want=1", len(hks))
				}
				resOut.Blob.HashKey = nil
				resp, err := gout.gomaFile.LookupFile(ctx, &gomapb.LookupFileReq{HashKey: hks})
				if err != nil {
					t.Errorf("LookupFile failed: %v", err)
				}
				// Make sure the stored blob is the same as the original blob.
				wantBlobs := []*gomapb.FileBlob{tc.output[i].Blob}
				if !cmp.Equal(resp.Blob, wantBlobs, cmp.Comparer(proto.Equal)) {
					t.Errorf("at i=%d, resp.Blob=%v, want=%v", i, resp.Blob[0], wantBlobs)
				}
			}

			for i, resOut := range result.Output {
				wantOut := tc.wantOutput[i]
				if !cmp.Equal(resOut, wantOut, cmp.Comparer(proto.Equal)) {
					t.Errorf("at i=%d, resOut=%v, want=%v", i, resOut, wantOut)
				}
			}
		})
	}
}
