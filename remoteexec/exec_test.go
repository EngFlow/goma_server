// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/hash"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/remoteexec/merkletree"
)

func TestSortMissing(t *testing.T) {
	inputs := []*gomapb.ExecReq_Input{
		{
			Filename: proto.String("../src/hello.cc"),
			HashKey:  proto.String("hash-hello.cc"),
		},
		{
			Filename: proto.String("../include/base.h"),
			HashKey:  proto.String("hash-base.h"),
		},
		{
			Filename: proto.String("../include/hello.h"),
			HashKey:  proto.String("hash-hello.h"),
		},
	}

	resp := &gomapb.ExecResp{
		MissingInput: []string{
			"../include/hello.h",
			"../src/hello.cc",
		},
		MissingReason: []string{
			"missing-hello.h",
			"missing-hello.cc",
		},
	}

	sortMissing(inputs, resp)
	want := &gomapb.ExecResp{
		MissingInput: []string{
			"../src/hello.cc",
			"../include/hello.h",
		},
		MissingReason: []string{
			"missing-hello.cc",
			"missing-hello.h",
		},
	}
	if !proto.Equal(resp, want) {
		t.Errorf("sortMissing: %s != %s", resp, want)
	}

	resp = proto.Clone(want).(*gomapb.ExecResp)
	sortMissing(inputs, resp)
	if !proto.Equal(resp, want) {
		t.Errorf("sortMissing (stable): %s != %s", resp, want)
	}
}

func TestChangeSymlinkAbsToRel(t *testing.T) {
	for _, tc := range []struct {
		desc       string
		name       string
		target     string
		wantTarget string
		wantErr    bool
	}{
		{
			desc:       "base",
			name:       "/a/b.txt",
			target:     "/c/d.txt",
			wantTarget: "../c/d.txt",
		},
		{
			desc:    "should be error because name is not absolute",
			name:    "../a/b.txt",
			target:  "/c/d.txt",
			wantErr: true,
		},
		{
			desc:       "should allow name is in root",
			name:       "/a.txt",
			target:     "/c/d.txt",
			wantTarget: "c/d.txt",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			e := merkletree.Entry{
				Name:   tc.name,
				Target: tc.target,
			}
			actual, err := changeSymlinkAbsToRel(e)
			if tc.wantErr && err == nil {
				t.Errorf("changeSymlinkAbsToRel(%v) returned nil err; want err", e)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("changeSymlinkAbsToRel(%v) returned %v ; want nil", e, err)
			}
			if tc.wantTarget != actual.Target {
				expected := merkletree.Entry{
					Name:   e.Name,
					Target: tc.wantTarget,
				}
				t.Errorf("changeSymlinkAbsToRel(%v) = %v; want %v", e, actual, expected)
			}
		})
	}
}

type fakeGomaInput struct {
	digests map[*gomapb.ExecReq_Input]digest.Data
	hashes  map[*gomapb.FileBlob]string
}

func (f *fakeGomaInput) setInputs(inputs []*gomapb.ExecReq_Input) {
	if f.digests == nil {
		f.digests = make(map[*gomapb.ExecReq_Input]digest.Data)
	}
	if f.hashes == nil {
		f.hashes = make(map[*gomapb.FileBlob]string)
	}
	for _, input := range inputs {
		f.digests[input] = digest.Bytes(input.GetFilename(), input.Content.Content)
		f.hashes[input.Content] = input.GetHashKey()
	}
}

func (f *fakeGomaInput) toDigest(ctx context.Context, in *gomapb.ExecReq_Input) (digest.Data, error) {
	d, ok := f.digests[in]
	if !ok {
		return nil, errors.New("not found")
	}
	return d, nil
}

func (f *fakeGomaInput) upload(ctx context.Context, blob *gomapb.FileBlob) (string, error) {
	h, ok := f.hashes[blob]
	if !ok {
		return "", errors.New("upload error")
	}
	return h, nil
}

type nopLogger struct{}

func (nopLogger) Debug(args ...interface{})                {}
func (nopLogger) Debugf(format string, arg ...interface{}) {}
func (nopLogger) Info(args ...interface{})                 {}
func (nopLogger) Infof(format string, arg ...interface{})  {}
func (nopLogger) Warn(args ...interface{})                 {}
func (nopLogger) Warnf(format string, arg ...interface{})  {}
func (nopLogger) Error(args ...interface{})                {}
func (nopLogger) Errorf(format string, arg ...interface{}) {}
func (nopLogger) Fatal(args ...interface{})                {}
func (nopLogger) Fatalf(format string, arg ...interface{}) {}
func (nopLogger) Sync() error                              { return nil }

func BenchmarkInputFiles(b *testing.B) {
	var inputs []*gomapb.ExecReq_Input
	for i := 0; i < 1000; i++ {
		content := fmt.Sprintf("content %d", i)
		blob := &gomapb.FileBlob{
			BlobType: gomapb.FileBlob_FILE.Enum(),
			Content:  []byte(content),
			FileSize: proto.Int64(int64(len(content))),
		}
		hashkey, err := hash.SHA256Proto(blob)
		if err != nil {
			b.Fatal(err)
		}
		inputs = append(inputs, &gomapb.ExecReq_Input{
			HashKey:  proto.String(hashkey),
			Filename: proto.String(fmt.Sprintf("input_%d", i)),
			Content:  blob,
		})
	}
	gi := &fakeGomaInput{}
	gi.setInputs(inputs)
	rootRel := func(filename string) (string, error) { return filename, nil }
	executableInputs := map[string]bool{}
	sema := make(chan struct{}, 5)
	ctx := log.NewContext(context.Background(), nopLogger{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := inputFiles(ctx, inputs, gi, rootRel, executableInputs, sema)
		if err != nil {
			b.Fatal(err)
		}
	}
}
