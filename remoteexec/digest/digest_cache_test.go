/* Copyright 2019 Google Inc. All Rights Reserved. */

package digest

import (
	"context"
	"testing"

	"go.chromium.org/goma/server/cache"
)

func TestCacheGet(t *testing.T) {
	c, err := cache.New(cache.Config{
		MaxBytes: 1 * 1024 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	dc := NewCache(cache.LocalClient{
		CacheServiceServer: c,
	}, 1000)

	ctx := context.Background()

	want := Bytes("first", []byte{12})
	_, err = dc.Get(ctx, "12", want)
	if err != nil {
		t.Fatalf("Get(ctx, 12, 'first')=%v; want nil error", err)
	}

	d2, err := dc.Get(ctx, "12", Bytes("second", []byte{12}))
	if err != nil {
		t.Fatalf("Get(ctx, 12, 'second')=%v; want nil error", err)
	}
	if d2.String() == want.String() {
		t.Errorf("Get(ctx, 12, 'second')=%v; want %v", d2, want)
	}
}
