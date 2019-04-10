// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package cache

import (
	"bytes"
	"context"
	"errors"
	"expvar"
	"sync"

	"cloud.google.com/go/storage"
	"github.com/golang/groupcache/lru"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"go.chromium.org/goma/server/cache/gcs"
	"go.chromium.org/goma/server/log"
	cachepb "go.chromium.org/goma/server/proto/cache"
)

// cache is a wrapper around an *lru.Cache that adds synchronization,
// and counts the size of all keys and values.
type memcache struct {
	MaxBytes   int64
	mu         sync.RWMutex
	nbytes     int64 // of all keys and vlaues
	lru        *lru.Cache
	nhit, nget int64
	nevict     int64 // number of evictions
	nreplace   int64
}

var errNoChange = errors.New("cache: no change")

type replaceError struct {
	old []byte
}

func (r replaceError) Error() string {
	return "cache: value is replaced"
}

// Put puts key-value pair in memcache.
// It returns errNoChange if key-value pair was already stored.
// It returns replaceError if value is replaced.
func (c *memcache) Put(ctx context.Context, key string, value []byte) error {
	span := trace.FromContext(ctx)
	span.Annotatef(nil, "put %s (size:%d)", key, len(value))
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.add(ctx, key, value)
	if c.MaxBytes == 0 {
		return err
	}
	for {
		if c.nbytes < c.MaxBytes {
			return err
		}
		span.Annotatef(nil, "eviction %d exceeding max=%d", c.nbytes, c.MaxBytes)
		c.lru.RemoveOldest()
	}
}

// add adds key-value pair in memcache.
// It returns errNoChange if key-value pair already exists in memcache.
// It returns replaceError if key exists but value differs.
func (c *memcache) add(ctx context.Context, key string, value []byte) error {
	logger := log.FromContext(ctx)

	if c.lru == nil {
		c.lru = &lru.Cache{
			OnEvicted: func(key lru.Key, value interface{}) {
				logger := log.FromContext(context.Background())
				logger.Infof("mem.evict %s %d", key.(string), len(value.([]byte)))
				c.nbytes -= int64(len(key.(string))) + int64(len(value.([]byte)))
				c.nevict++
			},
		}
	}
	var err error
	vi, ok := c.lru.Get(key)
	if ok {
		ov := vi.([]byte)
		if bytes.Equal(ov, value) {
			logger.Infof("mem.put2  %s %d", key, len(value))
			return errNoChange
		}
		logger.Errorf("mem.repl  %s %d <= %d", key, len(value), len(ov))
		// replace won't call OnEvicted.
		c.nbytes -= int64(len(key)) + int64(len(ov))
		c.nreplace++
		err = replaceError{old: ov}
	} else {
		logger.Infof("mem.put   %s %d", key, len(value))
	}
	c.lru.Add(key, value)
	c.nbytes += int64(len(key)) + int64(len(value))
	return err
}

func (c *memcache) Get(ctx context.Context, key string) (value []byte, ok bool) {
	span := trace.FromContext(ctx)
	span.Annotatef(nil, "get %s", key)
	logger := log.FromContext(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.nget++
	if c.lru == nil {
		logger.Infof("mem.miss  %s", key)
		return nil, false
	}
	vi, ok := c.lru.Get(key)
	if !ok {
		logger.Infof("mem.miss  %s", key)
		return nil, false
	}
	c.nhit++
	logger.Infof("mem.hit   %s %d", key, len(vi.([]byte)))
	return vi.([]byte), true
}

// TODO: use opencensus stats, view.
type memstats struct {
	MaxBytes int64

	Bytes    int64
	Num      int
	Hits     int64
	Gets     int64
	Evicts   int64
	Replaces int64
}

func (c *memcache) stats() memstats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var num int
	if c.lru != nil {
		num = c.lru.Len()
	}
	return memstats{
		MaxBytes: c.MaxBytes,
		Bytes:    c.nbytes,
		Num:      num,
		Hits:     c.nhit,
		Gets:     c.nget,
		Evicts:   c.nevict,
		Replaces: c.nreplace,
	}
}

// Config is a configuration for Cache.
type Config struct {
	// MaxBytes is maximum number of bytes used for cache.
	MaxBytes int64
	// TODO:
	// Dir          string
	// MaxDiskBytes int64

	Bucket *storage.BucketHandle
}

// TODO: put it in Config?
const writeBackSemaphore = 8

// Cache represents key-value cache.
type Cache struct {
	mem memcache
	gcs *gcs.Cache

	wbsema chan bool
}

var (
	mutex  sync.RWMutex
	caches []*Cache
)

// New creates new Cache for Config.
func New(c Config) (*Cache, error) {
	cache := &Cache{
		mem: memcache{
			MaxBytes: c.MaxBytes,
		},
	}

	if c.Bucket != nil {
		cache.gcs = gcs.New(c.Bucket)
		cache.wbsema = make(chan bool, writeBackSemaphore)
	}

	mutex.Lock()
	caches = append(caches, cache)
	mutex.Unlock()
	return cache, nil
}

// Put puts new key-value pair in memcache (always; i.e. overwrite existing one)
// and cloud cache (if gcs is configured, and new value is put).
// It returns error if it fails to put cache in cloud storage.
func (c *Cache) Put(ctx context.Context, req *cachepb.PutReq) (*cachepb.PutResp, error) {
	err := c.mem.Put(ctx, req.Kv.Key, req.Kv.Value)

	if err == errNoChange {
		return &cachepb.PutResp{}, nil
	}
	if c.gcs == nil {
		return &cachepb.PutResp{}, nil
	}
	if req.WriteBack {
		ctx, _ := trace.StartSpanWithRemoteParent(context.Background(), "go.chromium.org/goma/server/cache.Cache.Put.WriteBack", trace.FromContext(ctx).SpanContext())
		// TODO: pass tag?
		go func(ctx context.Context) {
			logger := log.FromContext(ctx)
			c.wbsema <- true
			defer func() {
				<-c.wbsema
			}()
			logger.Infof("gcs.put write back %s", req.Kv.Key)

			_, err := c.gcs.Put(ctx, req)
			if err != nil {
				logger.Errorf("gcs.put write back %s: %v", req.Kv.Key, err)
				return
			}
			logger.Infof("gcs.put write back %s: OK", req.Kv.Key)
		}(ctx)
		return &cachepb.PutResp{}, nil
	}

	return c.gcs.Put(ctx, req)
}

// Get gets key-value for requested key.
// It returns codes.NotFound if value not found in cache.
func (c *Cache) Get(ctx context.Context, req *cachepb.GetReq) (*cachepb.GetResp, error) {
	resp := &cachepb.GetResp{}
	v, ok := c.mem.Get(ctx, req.Key)
	if ok {
		resp.Kv = &cachepb.KV{
			Key:   req.Key,
			Value: v,
		}
		resp.InMemory = true
		return resp, nil
	}

	if req.Fast || c.gcs == nil {
		return nil, grpc.Errorf(codes.NotFound, "cache.Get: not found %s", req.Key)
	}
	resp, err := c.gcs.Get(ctx, req)
	if err != nil || resp.Kv == nil {
		return nil, grpc.Errorf(codes.NotFound, "cache.Get(%s): %v", req.Key, err)
	}
	c.mem.Put(ctx, req.Key, resp.Kv.Value)
	return resp, nil
}

type stats struct {
	Mem memstats
	GCS gcs.Stats
}

func (c *Cache) stats() stats {
	return stats{
		Mem: c.mem.stats(),
		GCS: c.gcs.Stats(),
	}
}

func publishStats() interface{} {
	mutex.RLock()
	defer mutex.RUnlock()
	var r []stats
	for _, cache := range caches {
		r = append(r, cache.stats())
	}
	return r
}

func init() {
	expvar.Publish("cache", expvar.Func(publishStats))
}
