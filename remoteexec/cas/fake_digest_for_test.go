// Copyright 2020 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package cas

import (
	"context"
	"errors"
	"io"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"go.chromium.org/goma/server/remoteexec/digest"
)

var (
	errFakeSourceNotFound = errors.New("Source not found")
)

type testSource struct {
	r io.ReadCloser
}

func (s testSource) Open(ctx context.Context) (io.ReadCloser, error) {
	if s.r == nil {
		return nil, errFakeSourceNotFound
	}
	return s.r, nil
}

func (s testSource) String() string {
	return ""
}

type testReadCloser struct {
	data []byte
}

func (rc *testReadCloser) Read(p []byte) (n int, err error) {
	n = copy(p, rc.data)
	return n, io.EOF
}

func (rc *testReadCloser) Close() error {
	return nil
}

type fakeDigestData struct {
	digest *rpb.Digest
	digest.Source
}

func (d *fakeDigestData) Digest() *rpb.Digest {
	return d.digest
}

func makeFakeDigestData(digest *rpb.Digest, data []byte) *fakeDigestData {
	var source testSource
	if data != nil {
		source.r = &testReadCloser{
			data: data,
		}
	}
	return &fakeDigestData{
		digest: digest,
		Source: source,
	}
}
