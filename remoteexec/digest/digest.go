/* Copyright 2018 Google Inc. All Rights Reserved. */

// Package digest handles content digest for remote executon API,
// https://github.com/bazelbuild/remote-apis/blob/c1c1ad2c97ed18943adb55f06657440daa60d833/build/bazel/remote/execution/v2/remote_execution.proto#L633
package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/hash"
	"go.chromium.org/goma/server/remoteexec/datasource"
)

// Data is data identified by digest.
type Data interface {
	Digest() *rpb.Digest
	Source
}

// Source accesses data source for the digest.
type Source interface {
	Open(context.Context) (io.ReadCloser, error)
	String() string
}

type data struct {
	digest *rpb.Digest
	source Source
}

// New creates digest data from source, which digest is d.
func New(src Source, d *rpb.Digest) Data {
	return data{
		digest: d,
		source: src,
	}
}

// Digest returns digest of the data.
func (d data) Digest() *rpb.Digest {
	return d.digest
}

// Open opens the data source.
func (d data) Open(ctx context.Context) (io.ReadCloser, error) {
	return d.source.Open(ctx)
}

func (d data) String() string {
	return fmt.Sprintf("%v %v", d.digest, d.source)
}

// Bytes creates data for bytes.
func Bytes(name string, b []byte) Data {
	h := hash.SHA256Content(b)
	return data{
		digest: &rpb.Digest{
			Hash:      h,
			SizeBytes: int64(len(b)),
		},
		source: datasource.Bytes(name, b),
	}
}

// Proto creates data for proto message.
func Proto(m proto.Message) (Data, error) {
	b, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}
	return Bytes(fmt.Sprintf("%T", m), b), nil
}

// FromSource creates digests from source.
func FromSource(ctx context.Context, src Source) (Data, error) {
	f, err := src.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return nil, err
	}
	return data{
		digest: &rpb.Digest{
			Hash:      hex.EncodeToString(h.Sum(nil)),
			SizeBytes: n,
		},
		source: src,
	}, nil
}
