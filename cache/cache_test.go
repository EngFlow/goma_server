// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package cache

import (
	"context"
	"testing"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "go.chromium.org/goma/server/proto/cache"
)

func TestGetPut(t *testing.T) {
	ctx := context.Background()
	cache, err := New(Config{
		MaxBytes: 1024 * 1024 * 1024,
	})

	if err != nil {
		t.Fatalf("cache.New(...): %v", err)
	}

	key := "key"

	kv := &pb.KV{
		Key:   key,
		Value: []byte("value"),
	}

	cache.Put(ctx, &pb.PutReq{
		Kv: kv,
	})

	getReq := &pb.GetReq{
		Key:  key,
		Fast: false,
	}

	gotResp, err := cache.Get(ctx, getReq)
	if err != nil {
		t.Errorf("cache.Get(%s): %v", key, err)
	}

	wantResp := &pb.GetResp{
		Kv:       kv,
		InMemory: true,
	}

	if !proto.Equal(gotResp, wantResp) {
		t.Errorf("got %#v; want %#v", gotResp, wantResp)
	}

}

func TestGetNotFound(t *testing.T) {
	ctx := context.Background()
	cache, err := New(Config{
		MaxBytes: 1024 * 1024 * 1024,
	})

	if err != nil {
		t.Fatalf("cache.New(...): %v", err)
	}

	key := "key"
	getReq := &pb.GetReq{
		Key:  key,
		Fast: false,
	}

	_, err = cache.Get(ctx, getReq)
	s, ok := status.FromError(err)
	if !ok || s.Code() != codes.NotFound {
		t.Errorf("cache.Get(%s): got %v, want NotFound error", key, err)
	}

}

func TestPutReplace(t *testing.T) {
	ctx := context.Background()
	cache, err := New(Config{
		MaxBytes: 1024 * 1024 * 1024,
	})

	if err != nil {
		t.Fatalf("cache.New(...): %v", err)
	}

	key := "key"

	kv := &pb.KV{
		Key:   key,
		Value: nil,
	}

	cache.Put(ctx, &pb.PutReq{
		Kv: kv,
	})

	getReq := &pb.GetReq{
		Key:  key,
		Fast: false,
	}

	gotResp, err := cache.Get(ctx, getReq)
	if err != nil {
		t.Errorf("cache.Get(%s): %v", key, err)
	}

	wantResp := &pb.GetResp{
		Kv:       kv,
		InMemory: true,
	}

	if !proto.Equal(gotResp, wantResp) {
		t.Errorf("got %#v; want %#v", gotResp, wantResp)
	}

	if got, want := cache.stats().Mem.Bytes, int64(len(key)); got != want {
		t.Errorf("Mem.Bytes=%d; want=%d", got, want)
	}

	t.Logf(`replace value to "value"`)

	kv.Value = []byte("value")

	cache.Put(ctx, &pb.PutReq{
		Kv: kv,
	})

	getReq = &pb.GetReq{
		Key:  key,
		Fast: false,
	}

	gotResp, err = cache.Get(ctx, getReq)
	if err != nil {
		t.Errorf("cache.Get(%s): %v", key, err)
	}

	wantResp = &pb.GetResp{
		Kv:       kv,
		InMemory: true,
	}

	if !proto.Equal(gotResp, wantResp) {
		t.Errorf("got %#v; want %#v", gotResp, wantResp)
	}

	st := cache.stats().Mem
	if got, want := st.Bytes, int64(len(key))+int64(len(kv.Value)); got != want {
		t.Errorf("Mem.Bytes=%d; want=%d", got, want)
	}
	if got, want := st.Replaces, int64(1); got != want {
		t.Errorf("Mem.Replaces=%d; want=%d", got, want)
	}

}
