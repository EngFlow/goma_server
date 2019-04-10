// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/hash"
	gomapb "go.chromium.org/goma/server/proto/api"
	filepb "go.chromium.org/goma/server/proto/file"
)

const (
	// LargeFileThreshold defines a number of bytes to use FILE_META instead of FILE.
	LargeFileThreshold = 2 * 1024 * 1024

	// FileChunkSize defines a size of each FILE_CHUNK content.
	FileChunkSize = 2 * 1024 * 1024
)

// ToLocal writes FileBlob contents in fname.
// If FileBlob is FILE_META, it will fetch FILE_CHUNK using FileServiceClient.
func ToLocal(ctx context.Context, fc filepb.FileServiceClient, blob *gomapb.FileBlob, fname string) error {
	f, err := os.Create(fname)
	if err != nil {
		return err
	}
	err = toLocal(ctx, fc, blob, f)
	cerr := f.Close()
	if err == nil && cerr != nil {
		err = cerr
	}
	return err
}

type writerAt interface {
	io.Writer
	io.WriterAt
}

func toLocal(ctx context.Context, fc filepb.FileServiceClient, blob *gomapb.FileBlob, w writerAt) error {
	var size int64
	switch blobType := blob.GetBlobType(); blobType {
	case gomapb.FileBlob_FILE:
		n, err := w.Write(blob.GetContent())
		if err != nil {
			return err
		}
		size = int64(n)

	case gomapb.FileBlob_FILE_META:
		// TODO: streaming?
		for i, hk := range blob.GetHashKey() {
			cresp, err := fc.LookupFile(ctx, &gomapb.LookupFileReq{
				HashKey: []string{hk},
			})
			if err != nil {
				return fmt.Errorf("chunk error: %d: %s: %v", i, hk, err)
			}
			if len(cresp.Blob) == 0 || !IsValid(cresp.Blob[0]) {
				return fmt.Errorf("missing chunk: %d %s", i, hk)
			}
			chunk := cresp.Blob[0]
			if chunk.GetBlobType() != gomapb.FileBlob_FILE_CHUNK {
				return fmt.Errorf("wrong blob type: %d: %s type=%v", i, hk, chunk.GetBlobType())
			}
			content := chunk.GetContent()
			n, err := w.WriteAt(content, chunk.GetOffset())
			if err != nil {
				return err
			}
			// assume chunks are in order (might have hole, but no random access).
			size += int64(n)
		}

	default:
		return fmt.Errorf("missing blob: %v", blobType)
	}

	if size != blob.GetFileSize() {
		return fmt.Errorf("partial written: %d != %d", size, blob.GetFileSize())
	}
	return nil
}

// FromLocal reads fname and fills in blob, and stores it in FileServiceClient.
func FromLocal(ctx context.Context, fc filepb.FileServiceClient, fname string, blob *gomapb.FileBlob) (os.FileInfo, error) {
	f, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	blob.FileSize = proto.Int64(fi.Size())
	return fi, FromReader(ctx, fc, f, blob)
}

// FromReader reads contents from r and fills in blob, and stores it in FileServiceClient if fc != nil.
// content size must be blob's FileSize.
func FromReader(ctx context.Context, fc filepb.FileServiceClient, r io.Reader, blob *gomapb.FileBlob) error {
	if blob.FileSize == nil {
		return errors.New("FileSize is not set")
	}
	if blob.GetFileSize() < LargeFileThreshold {
		blob.BlobType = gomapb.FileBlob_FILE.Enum()
		blob.Content = make([]byte, blob.GetFileSize())
		_, err := io.ReadFull(r, blob.Content)
		if err != nil {
			return err
		}
		return nil
	}

	blob.BlobType = gomapb.FileBlob_FILE_META.Enum()
	var offset int64
	for offset < blob.GetFileSize() {
		chunk := &gomapb.FileBlob{
			BlobType: gomapb.FileBlob_FILE_CHUNK.Enum(),
			FileSize: blob.FileSize,
			Offset:   proto.Int64(offset),
		}
		size := blob.GetFileSize() - offset
		if size > FileChunkSize {
			size = FileChunkSize
		}
		chunk.Content = make([]byte, size)
		_, err := io.ReadFull(r, chunk.Content)
		if err != nil {
			return err
		}
		var hk string
		if fc != nil {
			cresp, err := fc.StoreFile(ctx, &gomapb.StoreFileReq{
				Blob: []*gomapb.FileBlob{chunk},
			})
			if err != nil {
				return err
			}
			if len(cresp.HashKey) == 0 || cresp.HashKey[0] == "" {
				return fmt.Errorf("failed to store file offset=%d size=%d", offset, size)
			}
			hk = cresp.HashKey[0]
		} else {
			hk, err = hash.SHA256Proto(chunk)
			if err != nil {
				return fmt.Errorf("failed to compute hash of chunk offset=%d size=%d", offset, size)
			}
		}
		blob.HashKey = append(blob.HashKey, hk)
		offset += size
	}
	if fc == nil {
		return nil
	}
	cresp, err := fc.StoreFile(ctx, &gomapb.StoreFileReq{
		Blob: []*gomapb.FileBlob{blob},
	})
	if err != nil {
		return err
	}
	if len(cresp.HashKey) == 0 || cresp.HashKey[0] == "" {
		return fmt.Errorf("failed to store file_meta filesize=%d", blob.GetFileSize())
	}
	return nil
}

// BlobSpec represents a file by HashKey and/or Blob.
type BlobSpec struct {
	HashKey      string
	Blob         *gomapb.FileBlob
	IsExecutable bool
}

// Init initializes BlobSpec from HashKey or Blob.
// Either HashKey or Blob must be set before initialization.
func (b *BlobSpec) Init(ctx context.Context, client filepb.FileServiceClient) error {
	if b.Blob == nil && b.HashKey == "" {
		return errors.New("zero BlobSpec")
	}
	if b.Blob == nil {
		cresp, err := client.LookupFile(ctx, &gomapb.LookupFileReq{
			HashKey: []string{b.HashKey},
		})
		if err != nil {
			return err
		}
		if len(cresp.Blob) == 0 || !IsValid(cresp.Blob[0]) {
			return fmt.Errorf("no blob found for %s", b.HashKey)
		}
		b.Blob = cresp.Blob[0]
		return nil
	}
	if b.HashKey == "" {
		var err error
		b.HashKey, err = Key(b.Blob)
		if err != nil {
			return err
		}
		return nil
	}
	return nil
}

// Disk provides convenient methods to convert between local file and FileBlob in goma file service.
type Disk struct {
	Client filepb.FileServiceClient
}

// TODO: hard linkable cache.

// ToLocal creates file named fname from spec.
func (d Disk) ToLocal(ctx context.Context, spec *BlobSpec, fname string) error {
	err := spec.Init(ctx, d.Client)
	if err != nil {
		return fmt.Errorf("file.Disk: ToLocal %s: spec init failed: %v", fname, err)
	}
	err = ToLocal(ctx, d.Client, spec.Blob, fname)
	if err != nil {
		return fmt.Errorf("file.Disk: ToLocal %s: %v", fname, err)
	}
	return nil
}

// FromLocal fills spec from fname.
func (d Disk) FromLocal(ctx context.Context, fname string, spec *BlobSpec) error {
	spec.Blob = &gomapb.FileBlob{
		BlobType: gomapb.FileBlob_FILE_UNSPECIFIED.Enum(),
	}
	fi, err := FromLocal(ctx, d.Client, fname, spec.Blob)
	if err != nil {
		return err
	}
	spec.IsExecutable = fi.Mode().Perm()&0111 != 0
	return spec.Init(ctx, d.Client)
}
