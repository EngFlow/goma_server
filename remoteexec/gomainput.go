// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"sync"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/file"
	"go.chromium.org/goma/server/hash"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	fpb "go.chromium.org/goma/server/proto/file"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/rpc"
)

// TODO: test for FILE_META input.

type lookupClient interface {
	LookupFile(context.Context, *gomapb.LookupFileReq, ...grpc.CallOption) (*gomapb.LookupFileResp, error)
}

// gomaInput handles goma input files.
type gomaInput struct {
	gomaFile fpb.FileServiceClient

	// key: goma file hash -> value: digest.Data
	digestCache DigestCache

	mu   sync.Mutex
	srcs []*gomaInputSource
}

func (gi *gomaInput) Close() {
	gi.mu.Lock()
	defer gi.mu.Unlock()
	for _, src := range gi.srcs {
		src.resetBlob()
	}
}

// gomaInput converts goma input file to remoteexec digest.
func (gi *gomaInput) toDigest(ctx context.Context, input *gomapb.ExecReq_Input) (digest.Data, error) {
	hashKey := input.GetHashKey()
	// TODO: if input has size bytes, use it as digest.
	// if it has inlined content, put it in digest.Data.

	// client usually sets hashKey, but compute hashKey if not set.
	if hashKey == "" && input.GetContent() != nil {
		var err error
		hashKey, err = hash.SHA256Proto(input.GetContent())
		if err != nil {
			return nil, err
		}
	}
	src := &gomaInputSource{
		lookupClient: gi.gomaFile,
		hashKey:      hashKey,
		filename:     input.GetFilename(),
		blob:         input.GetContent(),
	}
	gi.mu.Lock()
	gi.srcs = append(gi.srcs, src)
	gi.mu.Unlock()

	return gi.digestCache.Get(ctx, hashKey, src)
}

func (gi *gomaInput) upload(ctx context.Context, content []*gomapb.FileBlob) ([]string, error) {
	if len(content) == 0 {
		return nil, status.Error(codes.FailedPrecondition, "upload: contents must not be empty.")
	}
	for _, c := range content {
		if c == nil {
			return nil, status.Error(codes.FailedPrecondition, "upload: contents must not be nil.")
		}
	}
	resp, err := gi.gomaFile.StoreFile(ctx, &gomapb.StoreFileReq{
		Blob: content,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.HashKey) < len(content) {
		return nil, status.Errorf(codes.Internal, "file.StoreFile: failed to set content: %d hashes returned, expected %d", len(resp.HashKey), len(content))
	}
	for _, hk := range resp.HashKey {
		if hk == "" {
			return nil, status.Errorf(codes.Internal, "file.StoreFile: failed to set content")
		}
	}
	return resp.HashKey, nil
}

func lookup(ctx context.Context, c lookupClient, hashKeys []string) ([]*gomapb.FileBlob, error) {
	req := &gomapb.LookupFileReq{
		HashKey:       hashKeys,
		RequesterInfo: requesterInfo(ctx),
	}
	var resp *gomapb.LookupFileResp
	var err error
	err = rpc.Retry{}.Do(ctx, func() error {
		resp, err = c.LookupFile(ctx, req)
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Blob) == 0 {
		return nil, status.Errorf(codes.NotFound, "no blob for %s", hashKeys)
	}
	if len(resp.Blob) != len(hashKeys) {
		return nil, status.Errorf(codes.Internal, "request %d (%q), got %d", len(hashKeys), hashKeys, len(resp.Blob))
	}
	var unspecified []string
	var blobs []*gomapb.FileBlob
	for i, blob := range resp.Blob {
		if blob.GetBlobType() == gomapb.FileBlob_FILE_UNSPECIFIED {
			unspecified = append(unspecified, fmt.Sprintf("%d:%q", i, hashKeys[i]))
			continue
		}
		blobs = append(blobs, blob)
	}
	if len(unspecified) > 0 {
		return nil, status.Errorf(codes.NotFound, "missing blob for %s", unspecified)
	}
	return blobs, nil
}

type gomaInputSource struct {
	lookupClient lookupClient
	hashKey      string
	filename     string

	mu   sync.Mutex
	blob *gomapb.FileBlob
}

func (g *gomaInputSource) String() string {
	g.mu.Lock()
	blob := g.blob
	g.mu.Unlock()
	return fmt.Sprintf("goma-input:%s %s %p", g.hashKey, g.filename, blob)
}

func (g *gomaInputSource) Filename() string {
	return g.filename
}

func (g *gomaInputSource) getBlob(ctx context.Context) (*gomapb.FileBlob, error) {
	g.mu.Lock()
	blob := g.blob
	g.mu.Unlock()
	if blob == nil {
		blobs, err := lookup(ctx, g.lookupClient, []string{g.hashKey})
		if err != nil {
			return nil, err
		}
		blob = blobs[0]
	}
	return blob, nil
}

func (g *gomaInputSource) resetBlob() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.blob = nil
}

func (g *gomaInputSource) Open(ctx context.Context) (io.ReadCloser, error) {
	blob, err := g.getBlob(ctx)
	if err != nil {
		return nil, err
	}
	switch blob.GetBlobType() {
	case gomapb.FileBlob_FILE:
		r := bytes.NewReader(blob.Content)
		return ioutil.NopCloser(r), nil

	case gomapb.FileBlob_FILE_META:
		return &gomaInputReader{
			ctx:          ctx,
			lookupClient: g.lookupClient,
			hashKey:      g.hashKey,
			meta:         blob,
		}, nil

	case gomapb.FileBlob_FILE_UNSPECIFIED:
		return nil, status.Errorf(codes.NotFound, "missing blob for %s", g.hashKey)
	}
	return nil, status.Errorf(codes.Internal, "bad file_blob type: %s: %v", g.hashKey, blob.GetBlobType())
}

const gomaInputBatchSize = 5

// one allocation at most 10MiB (5 * 2MB)
// limit at most 1GB (5 * 2MiB * 100) for input buffers.
const maxConcurrentInputBuffers = 100

type gomaInputBufferPool struct {
	sema chan bool
	// use sync.Pool?
}

var inputBufferPool = &gomaInputBufferPool{
	sema: make(chan bool, maxConcurrentInputBuffers),
}

func (p *gomaInputBufferPool) allocate(ctx context.Context, size int64) ([]byte, error) {
	if size > gomaInputBatchSize*file.FileChunkSize {
		size = gomaInputBatchSize * file.FileChunkSize
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	start := time.Now()
	select {
	case <-ctx.Done():
		logger := log.FromContext(ctx)
		logger.Errorf("goma input allocate buffer timed-out size=%d, %s", size, time.Since(start))
		stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(allocStatusKey, "fail"),
		}, inputBufferAllocSize.M(size))
		return nil, ctx.Err()
	case p.sema <- true:
		stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(allocStatusKey, "ok"),
		}, inputBufferAllocSize.M(size))
		return make([]byte, size), nil
	}
}

func (p *gomaInputBufferPool) release(buf []byte) {
	<-p.sema
}

type gomaInputReader struct {
	ctx          context.Context
	lookupClient lookupClient
	hashKey      string
	meta         *gomapb.FileBlob
	i            int    // next index of hash key in meta.
	buf          []byte // points meta hash_key[prev_i:i]'s Content.
	allocated    []byte // allocated buffer for buf.
}

func (r *gomaInputReader) Read(buf []byte) (int, error) {
	if len(r.buf) == 0 {
		if len(r.meta.HashKey) == r.i {
			// release buffer once all contents has been processed.
			inputBufferPool.release(r.allocated)
			r.buf = nil
			r.allocated = nil
			return 0, io.EOF
		}
		j := r.i + gomaInputBatchSize
		if j > len(r.meta.HashKey) {
			j = len(r.meta.HashKey)
		}
		blobs, err := lookup(r.ctx, r.lookupClient, r.meta.HashKey[r.i:j])
		if err != nil {
			return 0, status.Errorf(status.Code(err), "lookup chunk in FILE_META %s %d:%d %s: %v", r.hashKey, r.i, j, r.meta.HashKey[r.i:j], err)
		}
		for i, blob := range blobs {
			if blob.GetBlobType() != gomapb.FileBlob_FILE_CHUNK {
				return 0, status.Errorf(codes.Internal, "lookup chunk in FILE_META %s %d %s: not FILE_CHUNK %v", r.hashKey, r.i+i, r.meta.HashKey[r.i+i], blob.GetBlobType())
			}
		}
		if len(r.allocated) == 0 {
			r.allocated, err = inputBufferPool.allocate(r.ctx, r.meta.GetFileSize())
			if err != nil {
				return 0, status.Errorf(codes.ResourceExhausted, "allocate buffer for FILE_META %s size=%d: %v", r.hashKey, r.meta.GetFileSize(), err)
			}
		}
		b := r.allocated
		pos := 0
		i0 := r.i
		r.i = j
		for i, blob := range blobs {
			n := copy(b[pos:], blob.Content)
			if n < len(blob.Content) {
				return 0, status.Errorf(codes.Internal, "goma input buffer shortage %d written, len(blob.Content)=%d for %s %d %s", n, len(blob.Content), r.hashKey, i0+i, r.meta.HashKey[i0+i])
			}
			pos += n
		}
		r.buf = b[:pos]
	}
	n := copy(buf, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *gomaInputReader) Close() error {
	// release buffer if it has not yet been released.
	if len(r.allocated) > 0 {
		inputBufferPool.release(r.allocated)
		r.buf = nil
		r.allocated = nil
	}
	return nil
}
