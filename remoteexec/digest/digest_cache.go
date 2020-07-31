/* Copyright 2018 Google Inc. All Rights Reserved. */

package digest

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/golang/groupcache/lru"
	"github.com/golang/protobuf/proto"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"go.chromium.org/goma/server/log"
	cachepb "go.chromium.org/goma/server/proto/cache"
)

var (
	cacheStats = stats.Int64(
		"go.chromium.org/goma/server/remoteexec/digest.cache",
		"digest cache operations",
		stats.UnitDimensionless)

	opKey      = tag.MustNewKey("op")
	fileExtKey = tag.MustNewKey("file_ext")

	DefaultViews = []*view.View{
		{
			Name:        "go.chromium.org/goma/server/remoteexec/digest.cache-entries",
			Description: `number of digest cache entries`,
			Measure:     cacheStats,
			Aggregation: view.Sum(),
		},
		{
			Name:        "go.chromium.org/goma/server/remoteexec/digest.cache-ops",
			Description: `digest cache operations`,
			Measure:     cacheStats,
			TagKeys: []tag.Key{
				opKey,
				fileExtKey,
			},
			Aggregation: view.Count(),
		},
	}
)

// Cache caches file's digest data.
type Cache struct {
	c cachepb.CacheServiceClient

	mu  sync.Mutex
	lru lru.Cache
}

// NewCache creates new cache for digest data.
func NewCache(c cachepb.CacheServiceClient, maxEntries int) *Cache {
	cache := &Cache{
		c: c,
	}
	cache.lru.MaxEntries = maxEntries
	cache.lru.OnEvicted = cache.onEvicted
	return cache
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
	var fileExt string
	if gi, ok := src.(interface {
		Filename() string
	}); ok {
		fileExt = filepathExt(gi.Filename())
	}

	if c != nil {
		c.mu.Lock()
		data, ok := c.lru.Get(lru.Key(key))
		c.mu.Unlock()
		if ok {
			stats.RecordWithTags(ctx, []tag.Mutator{
				tag.Upsert(opKey, "hit"),
				tag.Upsert(fileExtKey, fileExt),
			}, cacheStats.M(0))
			// stochastically put it to cache client
			// to make lru/lfu work?
			return data.(Data), nil
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
			c.lru.Add(lru.Key(key), d)
			c.mu.Unlock()
			stats.RecordWithTags(ctx, []tag.Mutator{
				tag.Upsert(opKey, "cache-get"),
				tag.Upsert(fileExtKey, fileExt),
			}, cacheStats.M(1))
		}
		return d, nil
	}
	logger.Infof("digest cache miss %s %v: %s", keystr, err, time.Since(start))
	stats.RecordWithTags(ctx, []tag.Mutator{
		tag.Upsert(opKey, "miss"),
		tag.Upsert(fileExtKey, fileExt),
	}, cacheStats.M(0))
	d, err := FromSource(ctx, src)
	if err != nil {
		logger.Warnf("digest from source %s %v: %s", keystr, err, time.Since(start))
		return nil, err
	}
	if c != nil {
		c.mu.Lock()
		c.lru.Add(lru.Key(key), d)
		c.mu.Unlock()
		logger.Infof("digest cache set %s => %v: %s", keystr, d, time.Since(start))
		err = c.cacheSet(ctx, key, d.Digest())
		if err != nil {
			logger.Warnf("digest cache set fail %s => %v: %v: %s", keystr, d, err, time.Since(start))
		}
		stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(opKey, "cache-set"),
			tag.Upsert(fileExtKey, fileExt),
		}, cacheStats.M(1))
	}
	return d, nil
}

func (c *Cache) onEvicted(k lru.Key, value interface{}) {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	key := k.(string)
	var filename, fileExt string
	d := value.(Data)
	if dd, ok := d.(data); ok {
		src := dd.source
		if gi, ok := src.(interface {
			Filename() string
		}); ok {
			filename = gi.Filename()
			fileExt = filepathExt(gi.Filename())
		}
	}
	logger.Infof("digest cache evict %s %s %q", key, d.Digest(), filename)
	stats.RecordWithTags(ctx, []tag.Mutator{
		tag.Upsert(opKey, "evict"),
		tag.Upsert(fileExtKey, fileExt),
	}, cacheStats.M(-1))
}

func filepathExt(fname string) string {
	ext := filepath.Ext(fname)
	if strings.ContainsAny(ext, `/\`) {
		// ext should not have path separtor.
		// if it has, it would be in directory part.
		ext = ""
	}
	return ext
}
