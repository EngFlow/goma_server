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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/hash"
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
}

// gomaInput converts goma input file to remoteexec digest.
func (gi gomaInput) toDigest(ctx context.Context, input *gomapb.ExecReq_Input) (digest.Data, error) {
	hashKey := input.GetHashKey()
	// TODO: if input has size bytes, use it as digest.
	// if it has inlined content, put it in digest.Data.

	if hashKey == "" && input.GetContent() != nil {
		var err error
		hashKey, err = hash.SHA256Proto(input.GetContent())
		if err != nil {
			return nil, err
		}
	}
	src := gomaInputSource{
		lookupClient: gi.gomaFile,
		hashKey:      hashKey,
		blob:         input.GetContent(),
	}
	return gi.digestCache.Get(ctx, hashKey, src)
}

func (gi gomaInput) upload(ctx context.Context, content *gomapb.FileBlob) (string, error) {
	if content == nil {
		return "", status.Error(codes.FailedPrecondition, "upload: contents must not be nil.")
	}
	resp, err := gi.gomaFile.StoreFile(ctx, &gomapb.StoreFileReq{
		Blob: []*gomapb.FileBlob{
			content,
		},
	})
	if err != nil {
		return "", err
	}
	if len(resp.HashKey) == 0 || resp.HashKey[0] == "" {
		return "", status.Errorf(codes.Internal, "file.StoreFile: failed to set content")
	}
	return resp.HashKey[0], nil
}

func lookup(ctx context.Context, c lookupClient, hashKey string) (*gomapb.FileBlob, error) {
	var resp *gomapb.LookupFileResp
	var err error
	err = rpc.Retry{}.Do(ctx, func() error {
		resp, err = c.LookupFile(ctx, &gomapb.LookupFileReq{
			HashKey: []string{
				hashKey,
			},
			RequesterInfo: requesterInfo(ctx),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Blob) == 0 {
		return nil, status.Errorf(codes.NotFound, "no blob for %s", hashKey)
	}
	if resp.Blob[0].GetBlobType() == gomapb.FileBlob_FILE_UNSPECIFIED {
		return nil, status.Errorf(codes.NotFound, "missing blob for %s", hashKey)
	}
	return resp.Blob[0], nil
}

type gomaInputSource struct {
	lookupClient lookupClient
	hashKey      string
	blob         *gomapb.FileBlob
}

func (g gomaInputSource) String() string {
	return fmt.Sprintf("goma-input:%s %p", g.hashKey, g.blob)
}

func (g gomaInputSource) Open(ctx context.Context) (io.ReadCloser, error) {
	blob := g.blob
	// TODO: release g.blob
	if blob == nil {
		var err error
		blob, err = lookup(ctx, g.lookupClient, g.hashKey)
		if err != nil {
			return nil, err
		}
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

type gomaInputReader struct {
	ctx          context.Context
	lookupClient lookupClient
	hashKey      string
	meta         *gomapb.FileBlob
	i            int    // next index of hash key in meta.
	buf          []byte // points meta hash_key[i-1]'s Content.
}

func (r *gomaInputReader) Read(buf []byte) (int, error) {
	if len(r.buf) == 0 {
		if len(r.meta.HashKey) == r.i {
			return 0, io.EOF
		}
		blob, err := lookup(r.ctx, r.lookupClient, r.meta.HashKey[r.i])
		if err != nil {
			return 0, status.Errorf(status.Code(err), "lookup chunk in FILE_META %s %d %s: %v", r.hashKey, r.i, r.meta.HashKey[r.i], err)
		}
		if blob.GetBlobType() != gomapb.FileBlob_FILE_CHUNK {
			return 0, status.Errorf(codes.Internal, "lookup chunk in FILE_META %s %d %s: not FILE_CHUNK %v", r.hashKey, r.i, r.meta.HashKey[r.i], blob.GetBlobType())
		}
		r.i++
		r.buf = blob.Content
	}
	n := copy(buf, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *gomaInputReader) Close() error {
	return nil
}
