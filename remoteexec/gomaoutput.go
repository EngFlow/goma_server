// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"github.com/golang/protobuf/proto"
	"go.opencensus.io/trace"
	"golang.org/x/sync/errgroup"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/file"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	fpb "go.chromium.org/goma/server/proto/file"
	"go.chromium.org/goma/server/remoteexec/cas"
	"go.chromium.org/goma/server/remoteexec/datasource"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/rpc"
)

// gomaOutput handles goma output files.
type gomaOutput struct {
	gomaResp *gomapb.ExecResp
	bs       bpb.ByteStreamClient
	instance string
	gomaFile fpb.FileServiceClient
}

func (g gomaOutput) stdoutData(ctx context.Context, eresp *rpb.ExecuteResponse) {
	if len(eresp.Result.StdoutRaw) > 0 {
		g.gomaResp.Result.StdoutBuffer = eresp.Result.StdoutRaw
		return
	}
	if eresp.Result.StdoutDigest == nil {
		return
	}
	var buf bytes.Buffer
	err := rpc.Retry{}.Do(ctx, func() error {
		err := cas.DownloadDigest(ctx, g.bs, &buf, g.instance, eresp.Result.StdoutDigest)
		return fixRBEInternalError(err)
	})
	if err != nil {
		logger := log.FromContext(ctx)
		logger.Errorf("failed to fetch stdout %v: %v", eresp.Result.StdoutDigest, err)
		g.gomaResp.ErrorMessage = append(g.gomaResp.ErrorMessage, fmt.Sprintf("failed to fetch stdout %v: %s", eresp.Result.StdoutDigest, status.Code(err)))
		return
	}
	g.gomaResp.Result.StdoutBuffer = buf.Bytes()
}

func (g gomaOutput) stderrData(ctx context.Context, eresp *rpb.ExecuteResponse) {
	if len(eresp.Result.StderrRaw) > 0 {
		g.gomaResp.Result.StderrBuffer = eresp.Result.StderrRaw
		return
	}
	if eresp.Result.StderrDigest == nil {
		return
	}
	var buf bytes.Buffer
	err := rpc.Retry{}.Do(ctx, func() error {
		err := cas.DownloadDigest(ctx, g.bs, &buf, g.instance, eresp.Result.StderrDigest)
		return fixRBEInternalError(err)
	})
	if err != nil {
		logger := log.FromContext(ctx)
		logger.Errorf("failed to fetch stderr %v: %v", eresp.Result.StdoutDigest, err)
		g.gomaResp.ErrorMessage = append(g.gomaResp.ErrorMessage, fmt.Sprintf("failed to fetch stderr %v: %s", eresp.Result.StderrDigest, status.Code(err)))
		return
	}
	g.gomaResp.Result.StderrBuffer = buf.Bytes()
}

func (g gomaOutput) outputFileHelper(ctx context.Context, fname string, output *rpb.OutputFile) (*gomapb.ExecResult_Output, error) {
	var blob *gomapb.FileBlob
	err := rpc.Retry{}.Do(ctx, func() error {
		var err error
		blob, err = g.toFileBlob(ctx, output)
		return fixRBEInternalError(err)
	})
	if err != nil {
		logger := log.FromContext(ctx)
		switch status.Code(err) {
		case codes.Unavailable, codes.Canceled, codes.Aborted:
			logger.Warnf("goma blob for %s: %v", output.Path, err)
		default:
			logger.Errorf("goma blob for %s: %v", output.Path, err)
		}
		return nil, fmt.Errorf("goma blob for %s: %v", output.Path, status.Code(err))
	}
	return &gomapb.ExecResult_Output{
		Filename:     proto.String(fname),
		Blob:         blob,
		IsExecutable: proto.Bool(output.IsExecutable),
	}, nil
}

func (g gomaOutput) outputFile(ctx context.Context, fname string, output *rpb.OutputFile) {
	result, err := g.outputFileHelper(ctx, fname, output)
	if err != nil {
		g.gomaResp.ErrorMessage = append(g.gomaResp.ErrorMessage, err.Error())
		return
	}
	g.gomaResp.Result.Output = append(g.gomaResp.Result.Output, result)
}

func (g gomaOutput) outputFilesConcurrent(ctx context.Context, outputs []*rpb.OutputFile, sema chan struct{}) {
	var wg sync.WaitGroup
	results := make([]*gomapb.ExecResult_Output, len(outputs))
	errs := make([]error, len(outputs))

	for i := range outputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sema <- struct{}{}
			defer func() {
				<-sema
			}()

			output := outputs[i]
			results[i], errs[i] = g.outputFileHelper(ctx, output.Path, output)
		}(i)
	}
	wg.Wait()

	for _, result := range results {
		if result != nil {
			g.gomaResp.Result.Output = append(g.gomaResp.Result.Output, result)
		}
	}
	for _, err := range errs {
		if err != nil {
			g.gomaResp.ErrorMessage = append(g.gomaResp.ErrorMessage, err.Error())
		}
	}
}

func toChunkedFileBlob(ctx context.Context, rd io.Reader, size int64, fs fpb.FileServiceClient) (*gomapb.FileBlob, error) {
	const bufsize = file.LargeFileThreshold
	in := bufio.NewReaderSize(rd, bufsize)
	blob := &gomapb.FileBlob{
		BlobType: gomapb.FileBlob_FILE_META.Enum(),
		FileSize: proto.Int64(size),
	}
	buf := make([]byte, bufsize)
	var offset int64
	eof := false
	for offset < size && !eof {
		remain := size - offset
		if remain < bufsize {
			buf = buf[:remain]
		}
		n, err := io.ReadFull(in, buf)
		if err != nil && err != io.EOF {
			return nil, err
		}
		eof = err == io.EOF
		var resp *gomapb.StoreFileResp
		err = rpc.Retry{}.Do(ctx, func() error {
			resp, err = fs.StoreFile(ctx, &gomapb.StoreFileReq{
				Blob: []*gomapb.FileBlob{
					{
						BlobType: gomapb.FileBlob_FILE_CHUNK.Enum(),
						Offset:   proto.Int64(offset),
						Content:  buf[:n],
						FileSize: proto.Int64(size),
					},
				},
				RequesterInfo: requesterInfo(ctx),
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("store blob failed offset=%d: %v", offset, err)
		}
		for _, hashKey := range resp.HashKey {
			if hashKey == "" {
				return nil, fmt.Errorf("store blob failed offset=%d", offset)
			}
			blob.HashKey = append(blob.HashKey, hashKey)
		}
		offset += int64(n)
	}
	if size != offset {
		return nil, fmt.Errorf("size mismatch: digest size=%d, store size=%d", size, offset)
	}
	// EOF is only returned when there is no more input available at the beginning of a read,
	// not at the end of the final non-empty read.
	n, err := in.Read(make([]byte, 1))
	if n != 0 {
		return nil, fmt.Errorf("more bytes were read past end: %d", n)
	}
	if err != io.EOF {
		return nil, fmt.Errorf("could not confirm EOF: %v", err)
	}
	return blob, nil
}

func (g gomaOutput) toFileBlob(ctx context.Context, output *rpb.OutputFile) (*gomapb.FileBlob, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/remoteexec.gomaOutput.toFileBlob")
	defer span.End()

	logger := log.FromContext(ctx)

	if output.Digest.SizeBytes < file.LargeFileThreshold {
		// for single FileBlob.
		var buf bytes.Buffer
		err := cas.DownloadDigest(ctx, g.bs, &buf, g.instance, output.Digest)
		if err != nil {
			return nil, err
		}
		return &gomapb.FileBlob{
			BlobType: gomapb.FileBlob_FILE.Enum(),
			Content:  buf.Bytes(),
			FileSize: proto.Int64(output.Digest.SizeBytes),
		}, nil
	}

	casErrCh := make(chan error, 1)
	rd, wr, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	go func() {
		err := cas.DownloadDigest(ctx, g.bs, wr, g.instance, output.Digest)
		if err != nil {
			switch status.Code(err) {
			case codes.Unavailable, codes.Canceled, codes.Aborted:
				logger.Warnf("cas download error %s: %v", output.Digest, err)
			default:
				logger.Errorf("cas download error %s: %v", output.Digest, err)
			}
		}
		wr.Close()
		casErrCh <- err
	}()

	blob, err := toChunkedFileBlob(ctx, rd, output.Digest.SizeBytes, g.gomaFile)
	if err != nil {
		return nil, fmt.Errorf("Failed to convert %v to chunked FileBlob: %v", output, err)
	}
	if err = <-casErrCh; err != nil {
		return nil, err
	}
	return blob, nil
}

func traverseTree(ctx context.Context, filepath clientFilePath, dname string, dir *rpb.Directory, ds *digest.Store) []*rpb.OutputFile {
	logger := log.FromContext(ctx)
	var result []*rpb.OutputFile
	for _, f := range dir.Files {
		fname := filepath.Join(dname, f.Name)
		result = append(result, &rpb.OutputFile{
			Path:         fname,
			Digest:       f.Digest,
			IsExecutable: f.IsExecutable,
		})
	}
	for _, d := range dir.Directories {
		subdname := filepath.Join(dname, d.Name)
		db, found := ds.Get(d.Digest)
		if !found {
			logger.Errorf("no dir for %s %s", subdname, d.Digest)
			continue
		}
		subdir := &rpb.Directory{}
		err := datasource.ReadProto(ctx, db, subdir)
		if err != nil {
			logger.Errorf("bad dir proto for %s %s: %v", subdname, d.Digest, err)
			continue
		}
		result = append(result, traverseTree(ctx, filepath, subdname, subdir, ds)...)
	}
	if len(dir.Symlinks) > 0 {
		logger.Errorf("symlinks exists in output dir %s: %q", dname, dir.Symlinks)
	}
	return result
}

func (g gomaOutput) outputDirectory(ctx context.Context, filepath clientFilePath, dname string, output *rpb.OutputDirectory, sema chan struct{}) {
	logger := log.FromContext(ctx)
	if output.TreeDigest == nil {
		logger.Warnf("no tree digest in %s", dname)
		return
	}
	var buf bytes.Buffer
	err := rpc.Retry{}.Do(ctx, func() error {
		err := cas.DownloadDigest(ctx, g.bs, &buf, g.instance, output.TreeDigest)
		return fixRBEInternalError(err)
	})
	if err != nil {
		logger.Errorf("failed to download tree %s: %v", dname, err)
		g.gomaResp.ErrorMessage = append(g.gomaResp.ErrorMessage, fmt.Sprintf("failed to download tree %s: %s", dname, status.Code(err)))
		return
	}
	tree := &rpb.Tree{}
	err = proto.Unmarshal(buf.Bytes(), tree)
	if err != nil {
		logger.Errorf("failed to unmarshal tree data %s: %v", dname, err)
		g.gomaResp.ErrorMessage = append(g.gomaResp.ErrorMessage, fmt.Sprintf("failed to unmarshal tree data %s: %v", dname, err))
		return
	}

	ds := digest.NewStore()
	for _, c := range tree.Children {
		d, err := digest.Proto(c)
		if err != nil {
			logger.Errorf("digest for children %s: %v", c, err)
			continue
		}
		ds.Set(d)
	}
	outputFiles := traverseTree(ctx, filepath, dname, tree.Root, ds)
	g.outputFilesConcurrent(ctx, outputFiles, sema)
}

// reduceRespSize attempts to reduce the encoded size of `g.gomaResp` to under `byteLimit`.
// It replaces all file blobs with blob_type=FILE (embedded data) with blob_type=FILE_REF
// (`content` in FileServer, `blob_type`=FILE). With each replaced blob, the response loses
// `sizeof(content)` bytes and gains `sizeof(hash_key)` bytes. This will result in a net
// reduction in encoded size as long as `sizeof(content)` > `sizeof(hash_key)`.
//
// See description of FileBlob for more information:
// https://chromium.googlesource.com/infra/goma/client/+/bd9711495c9357eead845f0ae2d4eef92494c6d5/lib/goma_data.proto#17
func (g gomaOutput) reduceRespSize(ctx context.Context, byteLimit int, sema chan struct{}) error {
	origSize := proto.Size(g.gomaResp)
	if origSize <= byteLimit {
		return nil
	}

	toStoredFileBlob := func(ctx context.Context, input []byte, fs fpb.FileServiceClient) (*gomapb.FileBlob, error) {
		size := int64(len(input))
		blob := &gomapb.FileBlob{
			BlobType: gomapb.FileBlob_FILE_REF.Enum(),
			FileSize: proto.Int64(size),
		}
		var resp *gomapb.StoreFileResp
		var err error
		err = rpc.Retry{}.Do(ctx, func() error {
			blob := &gomapb.FileBlob{
				BlobType: gomapb.FileBlob_FILE.Enum(),
				Content:  input,
				FileSize: proto.Int64(size),
			}
			resp, err = fs.StoreFile(ctx, &gomapb.StoreFileReq{
				Blob:          []*gomapb.FileBlob{blob},
				RequesterInfo: requesterInfo(ctx),
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("store blob failed: %v", err)
		}
		if len(resp.HashKey) != 1 {
			return nil, fmt.Errorf("store blob got len(resp.HashKey)=%d, want=1", len(resp.HashKey))
		}
		if resp.HashKey[0] == "" {
			return nil, fmt.Errorf("store blob failed with empty hash key")
		}
		blob.HashKey = resp.HashKey
		return blob, nil
	}

	output := g.gomaResp.Result.Output
	eg, ctx := errgroup.WithContext(ctx)
	// For simplicity, store all blobs in FileServer rather than worrying about which ones to
	// store.
	// TODO: We can optimize this later.
	for _, out := range output {
		out := out
		blob := out.Blob
		if blob.GetBlobType() != gomapb.FileBlob_FILE {
			continue
		}
		eg.Go(func() error {
			sema <- struct{}{}
			defer func() {
				<-sema
			}()

			newBlob, err := toStoredFileBlob(ctx, blob.Content, g.gomaFile)
			if err != nil {
				return err
			}
			out.Blob = newBlob
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	// The result could still be too big if there are many FILE_REF blobs.
	newSize := proto.Size(g.gomaResp)
	if newSize > byteLimit {
		return fmt.Errorf("new resp size: got=%d, want<=%d", newSize, byteLimit)
	}
	return nil
}
