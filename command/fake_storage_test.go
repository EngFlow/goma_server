// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package command

import (
	"context"
	"io/ioutil"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

func TestFakeStorageBucket(t *testing.T) {
	fs := newFakeStorage()
	bkt := fs.createBucket("my_bucket")

	if bkt == nil {
		t.Errorf("Unable to create Bucket my_bucket")
	}
	testValues := []struct {
		name    string
		value   string
		updated time.Time
	}{
		// The iterators will be sorted in order of `name`.
		{
			name:    "path/bar",
			value:   "BAR",
			updated: time.Date(2018, time.December, 20, 18, 07, 21, 0, time.UTC),
		},
		{
			name:    "path/foo",
			value:   "FOO",
			updated: time.Date(2018, time.December, 20, 17, 06, 20, 0, time.UTC),
		},
	}

	for _, tv := range testValues {
		bkt.storeString(tv.name, tv.value, tv.updated)
	}

	ctx := context.Background()
	t.Logf("Testing Object()")
	for _, tv := range testValues {
		obj := bkt.Object(tv.name)
		if obj.(*fakeObject) == nil {
			t.Errorf("Could not get Object %s", tv.name)
		}
		rd, err := obj.NewReader(ctx)
		data, err := ioutil.ReadAll(rd)
		rd.Close()
		if err != nil {
			t.Errorf("Error reading from Object %s: %v", tv.name, err)
		}
		if string(data) != tv.value {
			t.Errorf("Contents of Object %s, got=%s, want=%s", tv.name, string(data), tv.value)
		}
	}
	obj := bkt.Object("unknown")
	if obj != nil {
		// If this fails but prints obj=<nil>, it is because the return type was not
		// a true nil (interface var had type info, but not value)
		t.Errorf("Object(unknown)=%v, want=nil", obj)
	}

	t.Logf("Testing Objects()")
	iter := bkt.Objects(ctx, &storage.Query{
		Prefix: "path/",
	})
	for _, tv := range testValues {
		attr, err := iter.Next()
		if err != nil {
			t.Errorf("Iterator.Next() err: got=%v, want=nil", err)
		}
		if attr.Name != tv.name {
			t.Errorf("Iterator.Next() name: got=%s, want=%s", attr.Name, tv.name)
		}
		if attr.Updated != tv.updated {
			t.Errorf("Iterator.Next() name: got=%v, want=%v", attr.Updated, tv.updated)
		}
	}
	_, err := iter.Next()
	if err != iterator.Done {
		t.Errorf("Iterator.Next(): got %v, want %v", err, iterator.Done)
	}
}

func TestFakeStorage(t *testing.T) {
	fs := newFakeStorage()
	foo := fs.Bucket("foo")
	if foo != nil {
		// If this fails but prints foo=<nil>, it is because the return type was not
		// a true nil (interface var had type info, but not value)
		t.Errorf("Bucket(foo)=%v, want=nil", foo)
	}
	foo = fs.createBucket("foo")
	foo2 := fs.Bucket("foo")
	if foo2 == nil {
		t.Errorf("Bucket(foo)=nil, want=non-nil")
	}
	if foo != foo2 {
		t.Errorf("Duplicate bucket foo: %v vs %v", &foo, &foo2)
	}
}
