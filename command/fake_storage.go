// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package command

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/googleapis/google-cloud-go-testing/storage/stiface"
	"google.golang.org/api/iterator"
)

type fakeObject struct {
	stiface.ObjectHandle
	data    []byte
	updated time.Time
}

func (o *fakeObject) NewReader(context.Context) (stiface.Reader, error) {
	return &fakeObjectReader{
		size: len(o.data),
		rc:   ioutil.NopCloser(bytes.NewReader(o.data)),
	}, nil
}

type fakeObjectReader struct {
	stiface.Reader
	size int
	rc   io.ReadCloser
}

func (r *fakeObjectReader) Size() int64 { return int64(r.size) }

func (r *fakeObjectReader) Read(p []byte) (int, error) {
	return r.rc.Read(p)
}

func (r *fakeObjectReader) Close() error {
	return r.rc.Close()
}

type fakeObjIter struct {
	stiface.ObjectIterator
	attrs []*storage.ObjectAttrs
}

func (o *fakeObjIter) Next() (*storage.ObjectAttrs, error) {
	if len(o.attrs) == 0 {
		return nil, iterator.Done
	}
	oa, attrs := o.attrs[0], o.attrs[1:]
	o.attrs = attrs
	return oa, nil
}

type fakeStorageBucket struct {
	stiface.BucketHandle
	objs map[string]*fakeObject
}

func newFakeStorageBucket() *fakeStorageBucket {
	return &fakeStorageBucket{
		objs: make(map[string]*fakeObject),
	}
}

func (sb *fakeStorageBucket) store(obj string, data []byte, ts time.Time) {
	sb.objs[obj] = &fakeObject{
		data:    data,
		updated: ts,
	}
}

func (sb *fakeStorageBucket) storeString(obj string, value string, ts time.Time) {
	sb.store(obj, []byte(value), ts)
}

func (sb *fakeStorageBucket) Object(name string) stiface.ObjectHandle {
	if sb.objs[name] == nil {
		// Return explicit nil as interface value.
		return nil
	}
	return sb.objs[name]
}

func (sb *fakeStorageBucket) Objects(ctx context.Context, query *storage.Query) stiface.ObjectIterator {
	// Get stored objects in sorted order.
	names := []string{}
	for name := range sb.objs {
		names = append(names, name)
	}
	sort.Strings(names)

	iter := &fakeObjIter{}
	for _, name := range names {
		if !strings.HasPrefix(name, query.Prefix) {
			continue
		}
		obj := sb.objs[name]
		iter.attrs = append(iter.attrs, &storage.ObjectAttrs{
			Name:    name,
			Updated: obj.updated,
		})
	}
	return iter
}

type fakeStorage struct {
	stiface.Client
	buckets map[string]*fakeStorageBucket
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{
		buckets: make(map[string]*fakeStorageBucket),
	}
}

func (s *fakeStorage) createBucket(bucket string) *fakeStorageBucket {
	// Only create bucket if none exists. If it exists, just return existing bucket.
	if s.buckets[bucket] == nil {
		s.buckets[bucket] = newFakeStorageBucket()
	}
	return s.buckets[bucket]
}

func (s *fakeStorage) Bucket(name string) stiface.BucketHandle {
	if s.buckets[name] == nil {
		// Return explicit nil as interface value.
		return nil
	}
	return s.buckets[name]
}
