// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package file

import (
	"context"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"go.opencensus.io/trace"

	"go.chromium.org/goma/server/hash"
	"go.chromium.org/goma/server/log"

	gomapb "go.chromium.org/goma/server/proto/api"
	cachepb "go.chromium.org/goma/server/proto/cache"
)

const (
	// DefaultMaxMsgSize is max message size for file service.
	// file service will handle 2MB chunk * 5 chunks in a request.
	// grpc's default is 4MB.
	DefaultMaxMsgSize = 12 * 1024 * 1024
)

// Service represents goma file service.
type Service struct {
	// Cache is a fileblob storage.
	Cache cachepb.CacheServiceClient
}

// StoreFile stores FileBlob.
func (s *Service) StoreFile(ctx context.Context, req *gomapb.StoreFileReq) (*gomapb.StoreFileResp, error) {
	span := trace.FromContext(ctx)

	span.AddAttributes(trace.Int64Attribute("store_num", int64(len(req.GetBlob()))))

	logger := log.FromContext(ctx)
	logger.Debugf("requester %v", req.GetRequesterInfo())
	start := time.Now()

	resp := &gomapb.StoreFileResp{
		HashKey: make([]string, len(req.GetBlob())),
	}

	var wg sync.WaitGroup

	for i, blob := range req.GetBlob() {
		wg.Add(1)
		// TODO: limit goroutine if cache server is overloaded or many request consume many memory.
		go func(i int, blob *gomapb.FileBlob) {
			defer wg.Done()
			if !IsValid(blob) {
				span.Annotatef(nil, "%d: invalid blob", i)
				logger.Errorf("%d: invalid blob", i)
				return
			}
			t := time.Now()
			b, err := proto.Marshal(blob)
			if err != nil {
				span.Annotatef(nil, "%d: proto.Marshal %v", i, err)
				logger.Errorf("%d: proto.Marshal: %v", i, err)
				return
			}
			marshalTime := time.Since(t)
			t = time.Now()
			hashKey := hash.SHA256Content(b)
			hashTime := time.Since(t)
			t = time.Now()
			_, err = s.Cache.Put(ctx, &cachepb.PutReq{
				Kv: &cachepb.KV{
					Key:   hashKey,
					Value: b,
				},
			})
			putTime := time.Since(t)
			span.Annotatef(nil, "%d hashKey=%s: %v", i, hashKey, err)
			if err != nil {
				logger.Errorf("%d: cache.Put %s: %v", i, hashKey, err)
				return
			}
			resp.HashKey[i] = hashKey
			logger.Infof("%d: cache.Put %s: marshal:%s hash:%s put:%s", i, hashKey, marshalTime, hashTime, putTime)
		}(i, blob)
	}
	logger.Debugf("waiting store %d blobs", len(req.GetBlob()))
	wg.Wait()
	logger.Debugf("store %d blobs %s", len(req.GetBlob()), time.Since(start))
	return resp, nil
}

// LookupFile looks up FileBlob.
func (s *Service) LookupFile(ctx context.Context, req *gomapb.LookupFileReq) (*gomapb.LookupFileResp, error) {
	span := trace.FromContext(ctx)
	span.AddAttributes(trace.Int64Attribute("lookup_num", int64(len(req.GetHashKey()))))

	logger := log.FromContext(ctx)

	logger.Debugf("requester %v", req.GetRequesterInfo())
	start := time.Now()

	resp := &gomapb.LookupFileResp{
		Blob: make([]*gomapb.FileBlob, len(req.GetHashKey())),
	}

	var wg sync.WaitGroup

	for i, hashKey := range req.GetHashKey() {
		wg.Add(1)
		// TODO: limit goroutine if cache server is overloaded or many request consume many memory.
		go func(i int, hashKey string) {
			defer wg.Done()
			t := time.Now()
			resp.Blob[i] = &gomapb.FileBlob{
				BlobType: gomapb.FileBlob_FILE_UNSPECIFIED.Enum(),
			}
			r, err := s.Cache.Get(ctx, &cachepb.GetReq{
				Key: hashKey,
			})
			getTime := time.Since(t)
			t = time.Now()
			if err != nil {
				span.Annotatef(nil, "%d: hashKey=%s: %v", i, hashKey, err)
				logger.Warnf("%d: cache.Get %s: %v", i, hashKey, err)
				return
			}
			if len(r.Kv.Value) == 0 {
				span.Annotatef(nil, "%d: hashKey=%s not found", i, hashKey)
				logger.Errorf("%d: cache.Get %s: no value", i, hashKey)
				return
			}
			err = proto.Unmarshal(r.Kv.Value, resp.Blob[i])
			unmarshalTime := time.Since(t)
			if err != nil {
				span.Annotatef(nil, "%d: hashKey=%s: proto.Unmarshal %v", i, hashKey, err)
				logger.Errorf("%d: proto.Unmarshal %s: %v", i, hashKey, err)
				return
			}
			logger.Infof("%d: cache.Get %s: get:%s unmarshal:%s", i, hashKey, getTime, unmarshalTime)
		}(i, hashKey)
	}
	logger.Debugf("waiting lookup %d blobs", len(req.GetHashKey()))
	wg.Wait()
	logger.Debugf("lookup %d blobs %s", len(req.GetHashKey()), time.Since(start))

	return resp, nil
}
