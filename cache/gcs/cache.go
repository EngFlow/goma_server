// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package gcs

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math/rand"
	"sync/atomic"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"

	"go.chromium.org/goma/server/log"
	pb "go.chromium.org/goma/server/proto/cache"
)

// AdmissionController checks incoming request.
type AdmissionController interface {
	AdmitPut(context.Context, *pb.PutReq) error
}

type nullAdmissionController struct{}

func (nullAdmissionController) AdmitPut(context.Context, *pb.PutReq) error { return nil }

// Cache represents key-value cache using google cloud storage.
type Cache struct {
	bkt                 *storage.BucketHandle
	AdmissionController AdmissionController
	// should be accessed via stomic pkg.
	nhit, nget int64
}

// New creates new cache.
func New(bkt *storage.BucketHandle) *Cache {
	return &Cache{
		bkt:                 bkt,
		AdmissionController: nullAdmissionController{},
	}
}

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

func crc32cStr(s uint32) string {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, s)
	return base64.StdEncoding.EncodeToString(buf)
}

func md5sumStr(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// checkAttrs checks attr matches with value.
// use hashes for integrity check.
// https://cloud.google.com/storage/docs/hashes-etags
func checkAttrs(attr *storage.ObjectAttrs, value []byte) error {
	if attr.Size != int64(len(value)) {
		return fmt.Errorf("storage: size: attr:%d != value:%d", attr.Size, len(value))
	}
	crc32cSum := crc32.Checksum(value, crc32cTable)
	if attr.CRC32C != crc32cSum {
		return fmt.Errorf("storage: crc32: attr:%s != value:%s", crc32cStr(attr.CRC32C), crc32cStr(crc32cSum))
	}
	md5sum := md5.Sum(value)
	if !bytes.Equal(attr.MD5, md5sum[:]) {
		return fmt.Errorf("storage: md5: attr:%s != value:%s", md5sumStr(attr.MD5), md5sumStr(md5sum[:]))
	}
	return nil
}

func (c *Cache) put(ctx context.Context, obj *storage.ObjectHandle, key string, value []byte, t time.Time) (*pb.PutResp, error) {
	logger := log.FromContext(ctx)
	attr, err := obj.Attrs(ctx)
	if err == nil {
		err = checkAttrs(attr, value)
		if err == nil {
			logger.Infof("gcs.put   %s %d %s: no change gen:%d %d", key, len(value), time.Since(t), attr.Generation, attr.Metageneration)
			return &pb.PutResp{}, nil
		}
		if ctx.Err() != nil {
			logger.Infof("gcs.put  %s %d %s: %v", key, len(value), time.Since(t), err)
			return nil, err
		}
		// attr mismatch. need overwrite.
		logger.Errorf("gcs.put   %s %d %s: %v", key, len(value), time.Since(t), err)
		t = time.Now()
	}
	w := obj.NewWriter(ctx)
	w.CRC32C = crc32.Checksum(value, crc32cTable)
	w.SendCRC32C = true
	w.ChunkSize = len(value)
	if w.ChunkSize > googleapi.DefaultUploadChunkSize {
		w.ChunkSize = googleapi.DefaultUploadChunkSize
	}

	if _, err := w.Write(value); err != nil {
		w.CloseWithError(err)
		logger.Errorf("gcs.put   %s %d %s: write:%v", key, len(value), time.Since(t), err)
		return nil, err
	}
	if err := w.Close(); err != nil {
		logger.Errorf("gcs.put   %s %d %s: close:%v", key, len(value), time.Since(t), err)
		return nil, err
	}
	attr = w.Attrs()
	logger.Infof("gcs.put   %s %d %s crc32c:%s md5:%s gen:%d %d", key, len(value), time.Since(t), crc32cStr(attr.CRC32C), md5sumStr(attr.MD5), attr.Generation, attr.Metageneration)
	return &pb.PutResp{}, nil
}

func (c *Cache) Put(ctx context.Context, in *pb.PutReq) (*pb.PutResp, error) {
	logger := log.FromContext(ctx)
	if err := c.AdmissionController.AdmitPut(ctx, in); err != nil {
		logger.Warnf("admission error: %v", err)
		return nil, err
	}
	key := in.Kv.Key
	value := in.Kv.Value
	t := time.Now()

	obj := c.bkt.Object(key)
	for retry := 0; ; retry++ {
		resp, err := c.put(ctx, obj, key, value, t)
		if err == nil {
			return resp, err
		}
		var gerr *googleapi.Error
		if !errors.As(err, &gerr) {
			return resp, err
		}
		if gerr.Code != 429 {
			return resp, err
		}
		// https://cloud.google.com/storage/quotas#objects
		// an update limit on each object of once per second.
		// http://b/145956239 gcp rate limit exceeded?
		backoff := float64(500)
		for n := retry; n > 0; n-- {
			backoff *= 1.6
		}
		const maxBackoff = 2000.0
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
		backoff *= 1 + 0.2*(rand.Float64()*2-1)
		const minBackoff = 50
		if backoff < minBackoff {
			backoff = minBackoff
		}
		w := time.Duration(backoff) * time.Millisecond
		logger.Warnf("gcs.put rate limit for %s. backoff %s", key, w)
		select {
		case <-time.After(w):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (c *Cache) Get(ctx context.Context, in *pb.GetReq) (*pb.GetResp, error) {
	logger := log.FromContext(ctx)
	key := in.Key

	t := time.Now()

	atomic.AddInt64(&c.nget, 1)
	obj := c.bkt.Object(key)
	attr, err := obj.Attrs(ctx)
	if err == storage.ErrObjectNotExist {
		logger.Infof("gcs.miss  %s %s: %v", key, time.Since(t), err)
		return nil, err
	}
	if err != nil {
		logger.Errorf("gcs.attrs %s %s: %v", key, time.Since(t), err)
		return nil, err
	}

	r, err := obj.NewReader(ctx)
	if err != nil {
		logger.Errorf("gcs.miss  %s %s: %v", key, time.Since(t), err)
		return nil, err
	}
	defer r.Close()

	b := make([]byte, attr.Size)
	_, err = io.ReadFull(r, b)
	if err != nil {
		logger.Errorf("gcs.miss  %s %s: %v", key, time.Since(t), err)
		return nil, err
	}
	err = checkAttrs(attr, b)
	if err != nil {
		logger.Errorf("gcs.bad   %s %d %s: %v", key, len(b), time.Since(t), err)
		return nil, fmt.Errorf("key:%s %v", key, err)
	}
	atomic.AddInt64(&c.nhit, 1)
	logger.Infof("gcs.hit   %s %d %s", key, len(b), time.Since(t))
	return &pb.GetResp{
		Kv: &pb.KV{
			Key:   key,
			Value: b,
		},
	}, nil
}

// Stats represents stats of gcs.Cache.
// TODO: use opencensus stats, view.
type Stats struct {
	Hits int64
	Gets int64
}

func (c *Cache) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	return Stats{
		Hits: atomic.LoadInt64(&c.nhit),
		Gets: atomic.LoadInt64(&c.nget),
	}
}
