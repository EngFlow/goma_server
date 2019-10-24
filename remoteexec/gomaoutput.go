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

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"github.com/golang/protobuf/proto"
	"go.opencensus.io/trace"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

func (g gomaOutput) outputFile(ctx context.Context, fname string, output *rpb.OutputFile) {
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
		g.gomaResp.ErrorMessage = append(g.gomaResp.ErrorMessage, fmt.Sprintf("goma blob for %s: %v", output.Path, status.Code(err)))
		return
	}
	g.gomaResp.Result.Output = append(g.gomaResp.Result.Output, &gomapb.ExecResult_Output{
		Filename:     proto.String(fname),
		Blob:         blob,
		IsExecutable: proto.Bool(output.IsExecutable),
	})
}

func (g gomaOutput) toFileBlob(ctx context.Context, output *rpb.OutputFile) (*gomapb.FileBlob, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/remoteexec.gomaOutput.toFileBlob")
	defer span.End()

	logger := log.FromContext(ctx)

	const fileBlobChunkThresholdBytes = 2 * 1024 * 1024

	if output.Digest.SizeBytes < fileBlobChunkThresholdBytes {
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

	var in io.Reader
	casErrCh := make(chan error, 1)
	const bufsize = fileBlobChunkThresholdBytes
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
	in = bufio.NewReaderSize(rd, bufsize)
	blob := &gomapb.FileBlob{
		BlobType: gomapb.FileBlob_FILE_META.Enum(),
		FileSize: proto.Int64(output.Digest.SizeBytes),
	}
	buf := make([]byte, bufsize)
	var offset int64
	for offset < output.Digest.SizeBytes {
		remain := output.Digest.SizeBytes - offset
		if remain < bufsize {
			buf = buf[:remain]
		}
		n, err := io.ReadFull(in, buf)
		if err != nil && err != io.EOF {
			return nil, err
		}
		eof := err == io.EOF
		var resp *gomapb.StoreFileResp
		err = rpc.Retry{}.Do(ctx, func() error {
			resp, err = g.gomaFile.StoreFile(ctx, &gomapb.StoreFileReq{
				Blob: []*gomapb.FileBlob{
					{
						BlobType: gomapb.FileBlob_FILE_CHUNK.Enum(),
						Offset:   proto.Int64(offset),
						Content:  buf[:n],
						FileSize: proto.Int64(output.Digest.SizeBytes),
					},
				},
				RequesterInfo: requesterInfo(ctx),
			})
			return err
		})
		if err != nil {
			logger.Warnf("store blob failed offset=%d for %v: %v", offset, output, err)
			return nil, err
		}
		for _, hashKey := range resp.HashKey {
			if hashKey == "" {
				return nil, fmt.Errorf("store blob failed offset=%d for %v", offset, output)
			}
			blob.HashKey = append(blob.HashKey, hashKey)
		}
		offset += int64(n)
		if eof && offset < output.Digest.SizeBytes {
			break
		}
	}
	if err := <-casErrCh; err != nil {
		return nil, err
	}
	if output.Digest.SizeBytes != offset {
		return nil, fmt.Errorf("size mismatch: digest size=%d, store size=%d", output.Digest.SizeBytes, offset)
	}
	return blob, nil
}

func (g gomaOutput) outputDirectory(ctx context.Context, filepath clientFilePath, dname string, output *rpb.OutputDirectory) {
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
	g.traverseTree(ctx, filepath, dname, tree.Root, ds)
}

func (g gomaOutput) traverseTree(ctx context.Context, filepath clientFilePath, dname string, dir *rpb.Directory, ds *digest.Store) {
	logger := log.FromContext(ctx)
	for _, f := range dir.Files {
		fname := filepath.Join(dname, f.Name)
		g.outputFile(ctx, fname, &rpb.OutputFile{
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
		g.traverseTree(ctx, filepath, subdname, subdir, ds)
	}
	if len(dir.Symlinks) > 0 {
		logger.Errorf("symlinks exists in output dir %s: %q", dname, dir.Symlinks)
	}
}
