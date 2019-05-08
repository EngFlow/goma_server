// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"context"
	"fmt"
	"io"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	pb "go.chromium.org/goma/server/proto/command"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/remoteexec/merkletree"
)

// CmdStorage is an interface to retrieve cmd file contents.
type CmdStorage interface {
	Open(ctx context.Context, hash string) (io.ReadCloser, error)
}

// fileSpecToEntry converts filespec to merkletree entry.
func fileSpecToEntry(ctx context.Context, fs *pb.FileSpec, cmdStorage CmdStorage) (merkletree.Entry, error) {
	if fs.HashKey != "" || fs.Blob != nil {
		return merkletree.Entry{}, fmt.Errorf("%s: fileSpecToEntry used for goma input? %s %s", fs.Path, fs.HashKey, fs.Blob.GetBlobType())
	}
	if fs.Hash != "" && fs.Symlink != "" {
		return merkletree.Entry{}, fmt.Errorf("%s: FileSpec has both `Hash` and `Symlink` fields", fs.Path)
	}
	if fs.Hash == "" {
		if fs.Symlink != "" {
			// Symlink
			return merkletree.Entry{
				Name:   fs.Path,
				Target: fs.Symlink,
			}, nil
		}
		// dir
		return merkletree.Entry{
			Name: fs.Path,
		}, nil
	}
	d := &rpb.Digest{
		Hash:      fs.Hash,
		SizeBytes: fs.Size,
	}
	src := cmdFileObj{
		storage: cmdStorage,
		hash:    fs.Hash,
	}
	return merkletree.Entry{
		Name:         fs.Path,
		Data:         digest.New(src, d),
		IsExecutable: fs.IsExecutable,
	}, nil
}

type cmdFileObj struct {
	storage CmdStorage
	hash    string
}

func (o cmdFileObj) Open(ctx context.Context) (io.ReadCloser, error) {
	r, err := o.storage.Open(ctx, o.hash)
	return r, err
}

func (o cmdFileObj) String() string {
	return fmt.Sprintf("cmd-file:%s", o.hash)
}
