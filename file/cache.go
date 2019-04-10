// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package file

import (
	"context"
	"errors"
	"io/ioutil"
	"path/filepath"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"

	"go.chromium.org/goma/server/hash"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	filepb "go.chromium.org/goma/server/proto/file"
)

var (
	errInvalidFileBlob = errors.New("invalid FileBlob")
)

// LocalCache is a goma file service with local disk cache.
type LocalCache struct {
	// Client is goma file service client.
	// If it is nil, it only uses local disk cache.
	Client filepb.FileServiceClient

	// Dir is a directory for local disk cache.
	Dir string
}

func (c LocalCache) load(ctx context.Context, hk string) (*gomapb.FileBlob, error) {
	hpath := filepath.Join(c.Dir, hk)
	b, err := ioutil.ReadFile(hpath)
	if err != nil {
		return nil, err
	}
	blob := &gomapb.FileBlob{
		BlobType: gomapb.FileBlob_FILE_UNSPECIFIED.Enum(),
	}
	err = proto.Unmarshal(b, blob)
	if err != nil {
		return nil, err
	}
	return blob, nil
}

func (c LocalCache) save(ctx context.Context, blob *gomapb.FileBlob) (string, error) {
	if !IsValid(blob) {
		return "", errInvalidFileBlob
	}
	b, err := proto.Marshal(blob)
	if err != nil {
		return "", err
	}
	hashKey := hash.SHA256Content(b)
	hpath := filepath.Join(c.Dir, hashKey)
	err = ioutil.WriteFile(hpath, b, 0644)
	if err != nil {
		return "", err
	}
	return hashKey, nil
}

// LookupFile looks up FileBlob for requested hash keys.
// It it is found in local disk cache, it will be used.
// Otherwise and c.Client is not nil, ask c.Client.
func (c LocalCache) LookupFile(ctx context.Context, req *gomapb.LookupFileReq, opts ...grpc.CallOption) (*gomapb.LookupFileResp, error) {
	logger := log.FromContext(ctx)

	logger.Infof("lookup %d keys", len(req.HashKey))
	resp := &gomapb.LookupFileResp{
		Blob: make([]*gomapb.FileBlob, len(req.HashKey)),
	}
	// TODO: single LookupFile rpc.
	for i, hk := range req.HashKey {
		blob, err := c.load(ctx, hk)
		if err != nil {
			logger.Infof("lookup %d %s miss: %v", i, hk, err)
			if c.Client != nil {
				cresp, err := c.Client.LookupFile(ctx, &gomapb.LookupFileReq{
					HashKey: []string{hk},
				}, opts...)
				if err != nil {
					logger.Warnf("lookup %d %s rpc miss: %v", i, hk, err)
					continue
				}
				if len(cresp.Blob) == 0 {
					logger.Errorf("lookup %d %s no blob", i, hk)
					continue
				}

				if !IsValid(cresp.Blob[0]) {
					logger.Warnf("lookup %d %s invalid blob", i, hk)
					continue
				}

				blob = cresp.Blob[0]
				_, err = c.save(ctx, blob)
				if err != nil {
					logger.Errorf("lookup %d %s save: %v", i, hk, err)
				} else {
					logger.Infof("fileblob save %s", hk)
				}
			}
		}
		resp.Blob[i] = blob
	}
	logger.Infof("lookup done")
	return resp, nil
}

// StoreFile stores FileBlob in local disk, and c.Client if c.Client is not nil.
func (c LocalCache) StoreFile(ctx context.Context, req *gomapb.StoreFileReq, opts ...grpc.CallOption) (*gomapb.StoreFileResp, error) {
	logger := log.FromContext(ctx)

	logger.Infof("store %d blobs", len(req.Blob))
	resp := &gomapb.StoreFileResp{
		HashKey: make([]string, len(req.Blob)),
	}
	for i, blob := range req.Blob {
		hk, err := c.save(ctx, blob)
		if err != nil {
			logger.Errorf("save %d failed: %v", i, err)
		} else {
			logger.Infof("fileblob save %s", hk)
		}
		resp.HashKey[i] = hk
	}
	logger.Infof("save local done")
	if c.Client != nil {
		logger.Infof("save rpc")
		_, err := c.Client.StoreFile(ctx, req, opts...)
		if err != nil {
			logger.Errorf("save rpc failed: %v", err)
		}
	}
	return resp, nil
}
