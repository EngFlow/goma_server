// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"context"
	"testing"
	"time"

	gomapb "go.chromium.org/goma/server/proto/api"
)

func TestUploadWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	gi := gomaInput{
		gomaFile:    cluster.adapter.GomaFile,
		digestCache: cluster.adapter.DigestCache,
	}

	hk, err := gi.upload(ctx, &gomapb.FileBlob{
		BlobType: gomapb.FileBlob_FILE.Enum(),
		Content:  []byte("dummy"),
	})
	if err != nil {
		t.Errorf("gi.upload err=%v; want nil", err)
	}
	if hk == "" {
		t.Errorf("gi.upload returns hk=empty string; want non empty")
	}
}

func TestUploadShouldReturnErrorOnNilContent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	gi := gomaInput{
		gomaFile:    cluster.adapter.GomaFile,
		digestCache: cluster.adapter.DigestCache,
	}

	_, err = gi.upload(ctx, nil)
	if err == nil {
		t.Errorf("gi.upload err=nil; want error")
	}
}
