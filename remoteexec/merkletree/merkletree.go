/* Copyright 2018 Google Inc. All Rights Reserved. */

// Package merkletree operates on a merkle tree for remote execution API,
// https://github.com/bazelbuild/remote-apis/blob/c1c1ad2c97ed18943adb55f06657440daa60d833/build/bazel/remote/execution/v2/remote_execution.proto#L838
//
// see https://en.Wikipedia.org/wiki/Merkle_tree
package merkletree

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/remoteexec/digest"

	rpb "go.chromium.org/goma/server/proto/remote-apis/build/bazel/remote/execution/v2"
)

// MerkleTree represents a merkle tree.
type MerkleTree struct {
	filepath FilePath

	rootDir string
	root    *rpb.Directory

	// dirname to Directory
	m     map[string]*rpb.Directory
	store *digest.Store
}

// FilePath provides filepath functionalities.
type FilePath interface {
	IsAbs(path string) bool
	Join(elem ...string) string
	SplitElem(path string) []string
}

// New creates new merkle tree under rootDir with cas store.
func New(filepath FilePath, rootDir string, store *digest.Store) *MerkleTree {
	return &MerkleTree{
		filepath: filepath,
		rootDir:  rootDir,
		root:     &rpb.Directory{},
		m:        make(map[string]*rpb.Directory),
		store:    store,
	}
}

// RootDir returns root dir of merkle tree.
func (m *MerkleTree) RootDir() string {
	return m.rootDir
}

// Entry is an entry in the tree.
type Entry struct {
	// Name is relative path from root dir.
	// it might not be clean path.
	// 'dir1/../dir2/file' will create
	//  - 'dir1/'
	//  - 'dir2/'
	//  - 'dir2/file'
	// error if name goes out to root.
	Name string

	// Data is entry's content. `nil` for directories and symlinks.
	Data digest.Data

	// IsExecutable is true if the file is executable.
	// no need to set this for directory.
	IsExecutable bool

	// If the file is a symlink, then this should be set to the target of the symlink.
	Target string
}

var (
	// ErrAbsPath indicates name in Entry is absulute path.
	ErrAbsPath = errors.New("merkletree: absolute path name")

	// ErrAmbigFileSymlink indicates Entry has both `Data` and `Target` fields, cannot determine
	// whether it is File or Symlink.
	ErrAmbigFileSymlink = errors.New("merkletree: unable to determine file vs symlink")

	// ErrBadPath indicates name in Entry contains bad path component
	// e.g. "." or "..".
	ErrBadPath = errors.New("merkletree: bad path component")
)

type dirstate struct {
	name string
	dir  *rpb.Directory
}

// Set sets an entry.
// It may return ErrAbsPath/ErrAmbigFileSymlink/ErrBadPath as error.
func (m *MerkleTree) Set(entry Entry) error {
	if entry.Target != "" && entry.Data != nil {
		return ErrAmbigFileSymlink
	}
	fname := entry.Name
	if m.filepath.IsAbs(fname) {
		return ErrAbsPath
	}
	elems := m.filepath.SplitElem(fname)
	if len(elems) == 0 {
		if entry.Data != nil {
			return ErrBadPath
		}
		return nil
	}
	cur := dirstate{
		name: ".",
		dir:  m.root,
	}
	var dirstack []dirstate
	for {
		var name string
		name, elems = elems[0], elems[1:]
		if len(elems) == 0 {
			// a leaf.
			if entry.Data == nil {
				if name == "." {
					return nil
				}
				if name == ".." && len(dirstack) == 0 {
					return ErrBadPath
				}
				if name == ".." {
					return nil
				}
				if entry.Target != "" {
					cur.dir.Symlinks = append(cur.dir.Symlinks, &rpb.SymlinkNode{
						Name:   name,
						Target: entry.Target,
					})
					return nil
				}
				m.setDir(cur, name)
				return nil
			}
			if name == "." || name == ".." {
				return ErrBadPath
			}
			m.store.Set(entry.Data)
			cur.dir.Files = append(cur.dir.Files, &rpb.FileNode{
				Name:         name,
				Digest:       entry.Data.Digest(),
				IsExecutable: entry.IsExecutable,
			})
			return nil
		}
		if name == "." {
			continue
		}
		if name == ".." {
			if len(dirstack) == 0 {
				return ErrBadPath
			}
			cur, dirstack = dirstack[len(dirstack)-1], dirstack[:len(dirstack)-1]
			continue
		}
		dirstack = append(dirstack, cur)
		cur = m.setDir(cur, name)
	}
}

func (m *MerkleTree) setDir(cur dirstate, name string) dirstate {
	dirname := filepath.Join(cur.name, name)
	dir, exists := m.m[dirname]
	if !exists {
		dirnode := &rpb.DirectoryNode{
			Name: name,
			// compute digest later
		}
		cur.dir.Directories = append(cur.dir.Directories, dirnode)
		dir = &rpb.Directory{}
		m.m[dirname] = dir
	}
	return dirstate{name: dirname, dir: dir}
}

// Build builds merkle tree and returns root's digest.
func (m *MerkleTree) Build(ctx context.Context) (*rpb.Digest, error) {
	return m.buildTree(ctx, m.root, "")
}

// buildtree builds tree at curdir, which is located as dirname.
func (m *MerkleTree) buildTree(ctx context.Context, curdir *rpb.Directory, dirname string) (*rpb.Digest, error) {
	logger := log.FromContext(ctx)
	// FIXME: this is workaround for b/71495874
	if len(curdir.Files) == 0 && len(curdir.Symlinks) == 0 && len(curdir.Directories) == 0 {
		// empty dir.
		// foundry doesn't create empty directory.
		// http://b/80406381
		// but, there is case that empty directory must exist
		// http://b/80279190
		// to workaround this put dummy file to make sure empty dir
		// is created.
		logger.Warnf("add .keep_me in %s", dirname)
		emptyFile := digest.Bytes("empty file", nil)
		m.store.Set(emptyFile)
		curdir.Files = append(curdir.Files, &rpb.FileNode{
			Name:   ".keep_me",
			Digest: emptyFile.Digest(),
		})
	}
	// directory should not have duplicate name.
	// http://b/124693412
	names := map[string]proto.Message{}
	var files []*rpb.FileNode
	for _, f := range curdir.Files {
		p, found := names[f.Name]
		if found {
			if !proto.Equal(f, p) {
				return nil, fmt.Errorf("duplicate file %s in %s: %s != %s", f.Name, dirname, f, p)
			}
			// goma client might send duplicate entries such as
			//   dir/subdir/../otherdir/name
			//   dir/otherdir/name
			logger.Infof("duplicate file %s in %s: %s", f.Name, dirname, f)
			continue
		}
		names[f.Name] = f
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	curdir.Files = files

	var symlinks []*rpb.SymlinkNode
	for _, s := range curdir.Symlinks {
		p, found := names[s.Name]
		if found {
			if !proto.Equal(s, p) {
				return nil, fmt.Errorf("duplicate symlink %s in %s: %s != %s", s.Name, dirname, s, p)
			}
			logger.Errorf("duplicate symlink %s in %s: %s", s.Name, dirname, s)
			continue
		}
		names[s.Name] = s
		symlinks = append(symlinks, s)
	}
	sort.Slice(symlinks, func(i, j int) bool {
		return symlinks[i].Name < symlinks[j].Name
	})
	curdir.Symlinks = symlinks

	var dirs []*rpb.DirectoryNode
	for _, subdir := range curdir.Directories {
		dirname := filepath.Join(dirname, subdir.Name)
		dir, found := m.m[dirname]
		if !found {
			return nil, fmt.Errorf("directory not found: %s", dirname)
		}
		digest, err := m.buildTree(ctx, dir, dirname)
		if err != nil {
			return nil, err
		}
		subdir.Digest = digest

		p, found := names[subdir.Name]
		if found {
			if !proto.Equal(subdir, p) {
				return nil, fmt.Errorf("duplicate dir %s in %s: %s != %s", subdir.Name, dirname, subdir, p)
			}
			logger.Errorf("duplicate dir %s in %s: %s", subdir.Name, dirname, subdir)
			continue
		}
		names[subdir.Name] = subdir
		dirs = append(dirs, subdir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].Name < dirs[j].Name
	})
	curdir.Directories = dirs

	data, err := digest.Proto(curdir)
	if err != nil {
		return nil, fmt.Errorf("directory digest %s: %v", dirname, err)
	}
	m.store.Set(data)
	return data.Digest(), nil
}
