/* Copyright 2018 Google Inc. All Rights Reserved. */

package digest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/log"
	cachepb "go.chromium.org/goma/server/proto/cache"
)

// Cache caches file's digest data.
type Cache struct {
	c  cachepb.CacheServiceClient
	mu sync.RWMutex
	m  map[string]Data
}

// NewCache creates new cache for digest data.
func NewCache(c cachepb.CacheServiceClient) *Cache {
	return &Cache{
		c: c,
		m: make(map[string]Data),
	}
}

var errNoCacheClient = errors.New("no cache client")

func (c *Cache) cacheGet(ctx context.Context, key string) (*rpb.Digest, error) {
	if c == nil || c.c == nil {
		return nil, errNoCacheClient
	}
	resp, err := c.c.Get(ctx, &cachepb.GetReq{
		Key: key,
	})
	if err != nil {
		return nil, err
	}
	d := &rpb.Digest{}
	err = proto.Unmarshal(resp.Kv.Value, d)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (c *Cache) cacheSet(ctx context.Context, key string, d *rpb.Digest) error {
	if c == nil || c.c == nil {
		return errNoCacheClient
	}
	v, err := proto.Marshal(d)
	if err != nil {
		return err
	}
	_, err = c.c.Put(ctx, &cachepb.PutReq{
		Kv: &cachepb.KV{
			Key:   key,
			Value: v,
		},
	})
	return err
}

// Get gets source's digest.
func (c *Cache) Get(ctx context.Context, key string, src Source) (Data, error) {
	if c != nil {
		c.mu.RLock()
		d, ok := c.m[key]
		c.mu.RUnlock()
		if ok {
			return d, nil
		}
	}
	var keystr string
	if s := src.String(); strings.Contains(s, key) {
		keystr = s
	} else {
		keystr = fmt.Sprintf("key:%s src:%s", key, src)
	}
	start := time.Now()
	logger := log.FromContext(ctx)
	// singleflight?
	dk, err := c.cacheGet(ctx, key)
	if err == nil {
		logger.Infof("digest cache get %s => %v: %s", keystr, dk, time.Since(start))
		d := New(src, dk)
		if c != nil {
			c.mu.Lock()
			c.m[key] = d
			c.mu.Unlock()
		}
		return d, nil
	}
	logger.Infof("digest cache miss %s %v: %s", keystr, err, time.Since(start))
	d, err := FromSource(ctx, src)
	if err != nil {
		logger.Warnf("digest from source %s %v: %s", keystr, err, time.Since(start))
		return nil, err
	}
	if c != nil {
		c.mu.Lock()
		c.m[key] = d
		c.mu.Unlock()
		logger.Infof("digest cache set %s => %v: %s", keystr, d, time.Since(start))
		err = c.cacheSet(ctx, key, d.Digest())
		if err != nil {
			logger.Warnf("digest cache set fail %s => %v: %v: %s", keystr, d, err, time.Since(start))
		}
	}
	return d, nil
}
