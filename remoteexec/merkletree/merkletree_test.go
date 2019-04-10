/* Copyright 2018 Google Inc. All Rights Reserved. */

package merkletree

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	rpb "go.chromium.org/goma/server/proto/remote-apis/build/bazel/remote/execution/v2"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
	"go.chromium.org/goma/server/remoteexec/datasource"
	"go.chromium.org/goma/server/remoteexec/digest"
)

func TestRootDir(t *testing.T) {
	ds := digest.NewStore()
	mt := New(posixpath.FilePath{}, "/path/to/root", ds)

	if got, want := mt.RootDir(), "/path/to/root"; got != want {
		t.Errorf("mt.RootDir()=%q; want=%q", got, want)
	}
}

func TestSet(t *testing.T) {
	for _, tc := range []struct {
		Entry
		wantErr  bool
		wantName string
		wantNode proto.Message // one of *rpb.FileNode or *rpb.SymlinkNode
		wantDirs []string
	}{
		{
			Entry: Entry{
				Name:         "third_party/llvm-build/Release+Asserts/bin/clang",
				Data:         digest.Bytes("clang binary", []byte("clang binary")),
				IsExecutable: true,
			},
			wantName: "clang",
			wantNode: &rpb.FileNode{
				Name: "third_party/llvm-build/Release+Asserts/bin/clang",
			},
			wantDirs: []string{
				"third_party",
				"third_party/llvm-build",
				"third_party/llvm-build/Release+Asserts",
				"third_party/llvm-build/Release+Asserts/bin",
			},
		},
		{
			Entry: Entry{
				Name:   "third_party/llvm-build/Release+Asserts/bin/clang++",
				Target: "clang",
			},
			wantName: "clang++",
			wantNode: &rpb.SymlinkNode{
				Name:   "third_party/llvm-build/Release+Asserts/bin/clang++",
				Target: "clang",
			},
			wantDirs: []string{
				"third_party",
				"third_party/llvm-build",
				"third_party/llvm-build/Release+Asserts",
				"third_party/llvm-build/Release+Asserts/bin",
			},
		},
		{
			Entry: Entry{
				Name: "path/../name",
			},
			// create 'path' dir and 'name' dir.
			wantDirs: []string{
				"path",
				"name",
			},
		},
		{
			Entry: Entry{
				Name: "../path/name",
			},
			// out of root.
			wantErr: true,
		},
		{
			Entry: Entry{
				Name: "path/name/..",
			},
			// create 'path/name' dir.
			wantDirs: []string{
				"path",
				"path/name",
			},
		},
		{
			Entry: Entry{
				Name: "path/name/.",
			},
			wantDirs: []string{
				"path",
				"path/name",
			},
		},
		{
			Entry: Entry{
				Name: "path/./name",
			},
			wantDirs: []string{
				"path",
				"path/name",
			},
		},
		{
			Entry: Entry{
				Name: "path//name",
			},
			wantDirs: []string{
				"path",
				"path/name",
			},
		},
		{
			Entry: Entry{
				Name: "..",
			},
			// out of root.
			wantErr: true,
		},
		{
			Entry: Entry{
				Name: "path/name/.",
				Data: digest.Bytes("file", []byte("file")),
			},
			wantErr: true,
		},
		{
			Entry: Entry{
				Name: "path/name/..",
				Data: digest.Bytes("file", []byte("file")),
			},
			wantErr: true,
		},
		{
			Entry: Entry{
				Name: "path/name/../../foo",
			},
			// "foo" dir
			wantDirs: []string{
				"path",
				"path/name",
				"foo",
			},
		},
		{
			Entry: Entry{
				Name: "path/name/../../foo",
				Data: digest.Bytes("file", []byte("file")),
			},
			// "foo" file
			wantName: "foo",
			wantNode: &rpb.FileNode{
				Name: "foo",
			},
			wantDirs: []string{
				"path",
				"path/name",
			},
		},
		{
			Entry: Entry{
				Name: "path/name/../..",
			},
			// root dir
			wantDirs: []string{
				"path",
				"path/name",
			},
		},
		{
			Entry: Entry{
				Name: "path/name/../..",
				Data: digest.Bytes("file", []byte("file")),
			},
			// .. should not be file.
			wantErr: true,
		},
		{
			Entry: Entry{
				Name: "path/name/../../../path/foo",
			},
			// go outside of root.
			wantErr: true,
		},
		{
			Entry: Entry{
				Name: "/full/path/name",
			},
			wantErr: true,
		},
	} {
		ds := digest.NewStore()
		mt := New(posixpath.FilePath{}, "/path/to/root", ds)
		err := mt.Set(tc.Entry)
		if (err != nil) != tc.wantErr {
			t.Errorf("mt.Set(%v)=%v; want err=%t", tc.Entry, err, tc.wantErr)
		}
		if tc.wantErr {
			continue
		}
		t.Logf("check for mt.Set(%v)", tc.Entry)
		if tc.wantNode != nil {
			key := ""
			switch wantNode := tc.wantNode.(type) {
			case *rpb.FileNode:
				key = wantNode.Name
			case *rpb.SymlinkNode:
				key = wantNode.Name
			default:
				t.Fatalf("Wrong node type: %T", tc.wantNode)
			}
			node, err := getNode(mt, key)
			if err != nil {
				t.Errorf("node(%q)=%#v, %v; want node, nil", key, node, err)
			}
		}
		for _, dirname := range tc.wantDirs {
			node, err := getNode(mt, dirname)
			_, ok := node.(*rpb.Directory)
			if err != nil || !ok {
				t.Errorf("node(%q)=%#v, %v; want directory, nil", dirname, node, err)
			}
		}
		sort.Strings(tc.wantDirs)
		var dirs []string
		for k := range mt.m {
			dirs = append(dirs, k)
		}
		sort.Strings(dirs)
		if !reflect.DeepEqual(dirs, tc.wantDirs) {
			t.Errorf("dirs=%q; want=%q", dirs, tc.wantDirs)
		}
	}
}

func getNode(mt *MerkleTree, path string) (proto.Message, error) {
	elems := strings.Split(filepath.Clean(path), "/")
	cur := mt.root
	if len(elems) == 0 {
		return cur, nil
	}
	var paths []string
	for {
		var name string
		name, elems = elems[0], elems[1:]
		if len(elems) == 0 {
			for _, n := range cur.Files {
				if name == n.Name {
					return n, nil
				}
			}
			for _, n := range cur.Symlinks {
				if name == n.Name {
					return n, nil
				}
			}
			return cur, nil
		}
		paths = append(paths, name)
		var ok bool
		cur, ok = mt.m[strings.Join(paths, "/")]
		if !ok {
			return nil, fmt.Errorf("%s not found in %s", name, strings.Join(paths[:len(paths)-1], "/"))
		}
	}
}

func TestBuildInvalidEntry(t *testing.T) {
	ds := digest.NewStore()
	mt := New(posixpath.FilePath{}, "/path/to/root", ds)

	for _, ent := range []Entry{
		Entry{
			// Invalid Entry: Absolute path.
			Name:         "/usr/bin/third_party/llvm-build/Release+Asserts/bin/clang",
			Data:         digest.Bytes("clang binary", []byte("clang binary")),
			IsExecutable: true,
		},
		Entry{
			// Invalid Entry: has both `Data` and `Target` fields set.
			Name:   "third_party/llvm-build/Release+Asserts/bin/clang++",
			Data:   digest.Bytes("clang binary", []byte("clang binary")),
			Target: "clang",
		},
	} {
		err := mt.Set(ent)
		if err == nil {
			t.Fatalf("mt.Set(%q)=nil; want=(error)", ent.Name)
		}
	}
}

func TestBuild(t *testing.T) {
	ctx := context.Background()
	ds := digest.NewStore()
	mt := New(posixpath.FilePath{}, "/path/to/root", ds)

	for _, ent := range []Entry{
		Entry{
			Name:         "third_party/llvm-build/Release+Asserts/bin/clang",
			Data:         digest.Bytes("clang binary", []byte("clang binary")),
			IsExecutable: true,
		},
		Entry{
			Name:   "third_party/llvm-build/Release+Asserts/bin/clang++-1",
			Target: "clang",
		},
		Entry{
			Name:   "third_party/llvm-build/Release+Asserts/bin/clang++",
			Target: "clang",
		},
		Entry{
			Name: "base/build_time.h",
			Data: digest.Bytes("base_time.h", []byte("byte_time.h content")),
		},
		Entry{
			Name: "out/Release/obj/base",
			// directory
		},
		Entry{
			Name: "base/debug/debugger.cc",
			Data: digest.Bytes("debugger.cc", []byte("debugger.cc content")),
		},
		Entry{
			Name: "base/test/../macros.h",
			Data: digest.Bytes("macros.h", []byte("macros.h content")),
		},
		// de-dup for same content http://b/124693412
		Entry{
			Name: "third_party/skia/include/private/SkSafe32.h",
			Data: digest.Bytes("SkSafe32.h", []byte("SkSafe32.h content")),
		},
		Entry{
			Name: "third_party/skia/include/private/SkSafe32.h",
			Data: digest.Bytes("SkSafe32.h", []byte("SkSafe32.h content")),
		},
	} {
		err := mt.Set(ent)
		if err != nil {
			t.Fatalf("mt.Set(%q)=%v; want=nil", ent.Name, err)
		}
	}

	d, err := mt.Build(ctx)
	if err != nil {
		t.Fatalf("mt.Build()=_, %v; want=nil", err)
	}

	dir, err := openDir(ctx, ds, d)
	if err != nil {
		t.Fatalf("root %v not found: %v", d, err)
	}
	checkDir(ctx, t, ds, dir, "",
		nil,
		[]string{"base", "out", "third_party"},
		nil)
	baseDir := checkDir(ctx, t, ds, dir, "base",
		[]string{"build_time.h", "macros.h"},
		[]string{"debug", "test"},
		nil)

	checkDir(ctx, t, ds, baseDir, "debug",
		[]string{"debugger.cc"},
		nil, nil)
	// empty dir will have .keep_me file.
	// TODO: remove this when b/71495874 is fixed.
	checkDir(ctx, t, ds, baseDir, "test",
		[]string{".keep_me"},
		nil, nil)

	outDir := checkDir(ctx, t, ds, dir, "out", nil, []string{"Release"}, nil)
	releaseDir := checkDir(ctx, t, ds, outDir, "Release", nil, []string{"obj"}, nil)
	objDir := checkDir(ctx, t, ds, releaseDir, "obj", nil, []string{"base"}, nil)
	// TODO: make nil instead of []string{".keep_me"} when b/71495874 is fixed.
	checkDir(ctx, t, ds, objDir, "base", []string{".keep_me"}, nil, nil)

	tpDir := checkDir(ctx, t, ds, dir, "third_party", nil, []string{"llvm-build", "skia"}, nil)
	llvmDir := checkDir(ctx, t, ds, tpDir, "llvm-build", nil, []string{"Release+Asserts"}, nil)
	raDir := checkDir(ctx, t, ds, llvmDir, "Release+Asserts", nil, []string{"bin"}, nil)
	binDir := checkDir(ctx, t, ds, raDir, "bin", []string{"clang"}, nil, []string{"clang++", "clang++-1"})

	_, isExecutable, err := getDigest(binDir, "clang")
	if err != nil || !isExecutable {
		t.Errorf("clang is not executable: %t, %v; want: true, nil", isExecutable, err)
	}
	for _, symlink := range []string{"clang++", "clang++-1"} {
		_, _, err := getDigest(binDir, symlink)
		if err != nil {
			t.Errorf("%s not found", symlink)
		}
	}
	skiaDir := checkDir(ctx, t, ds, tpDir, "skia", nil, []string{"include"}, nil)
	skiaIncludeDir := checkDir(ctx, t, ds, skiaDir, "include", nil, []string{"private"}, nil)
	skiaPrivateDir := checkDir(ctx, t, ds, skiaIncludeDir, "private", []string{"SkSafe32.h"}, nil, nil)
	_, isExecutable, err = getDigest(skiaPrivateDir, "SkSafe32.h")
	if err != nil || isExecutable {
		t.Errorf("SkSafe32 is executable: %t %v; want: false, nil", isExecutable, err)
	}
}

func TestBuildDuplicateError(t *testing.T) {
	for _, tc := range []struct {
		desc string
		ents []Entry
	}{
		{
			desc: "dup file-file",
			ents: []Entry{
				Entry{
					Name: "dir/file1",
					Data: digest.Bytes("file1.1", []byte("file1.1")),
				},
				Entry{
					Name: "dir/file1",
					Data: digest.Bytes("file1.2", []byte("file1.2")),
				},
			},
		},
		{
			desc: "dup file-symlink",
			ents: []Entry{
				Entry{
					Name: "dir/foo",
					Data: digest.Bytes("foo file", []byte("foo file")),
				},
				Entry{
					Name:   "dir/foo",
					Target: "bar",
				},
			},
		},
		{
			desc: "dup file-dir",
			ents: []Entry{
				Entry{
					Name: "dir/foo",
					Data: digest.Bytes("foo file", []byte("foo file")),
				},
				Entry{
					Name: "dir/foo",
				},
			},
		},
		{
			desc: "dup symlink-dir",
			ents: []Entry{
				Entry{
					Name:   "dir/foo",
					Target: "bar",
				},
				Entry{
					Name: "dir/foo",
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ctx := context.Background()
			ds := digest.NewStore()
			mt := New(posixpath.FilePath{}, "/path/to/root", ds)
			for _, ent := range tc.ents {
				err := mt.Set(ent)
				if err != nil {
					t.Fatalf("mt.Set(%q)=%v; want=nil", ent.Name, err)
				}
			}
			d, err := mt.Build(ctx)
			if err == nil {
				t.Errorf("mt.Build()=%v, nil, want=error", d)
			}
		})
	}
}

func checkDir(ctx context.Context, t *testing.T, ds *digest.Store, pdir *rpb.Directory, name string,
	wantFiles []string, wantDirs []string, wantSymlinks []string) *rpb.Directory {
	t.Helper()
	t.Logf("check %s", name)
	dir := pdir
	if name != "" {
		d, _, err := getDigest(pdir, name)
		if err != nil {
			t.Fatalf("getDigest(pdir, %q)=_, _, %v; want nil err", name, err)
		}
		dir, err = openDir(ctx, ds, d)
		if err != nil {
			t.Fatalf("openDir(ds, %v)=_, %v; want nil err", d, err)
		}
	}
	t.Logf("dir: %s", dir)
	files, dirs, symlinks := readDir(dir)
	if !reflect.DeepEqual(files, wantFiles) {
		t.Errorf("files=%q; want=%q", files, wantFiles)
	}
	if !reflect.DeepEqual(dirs, wantDirs) {
		t.Errorf("dirs=%q; want=%q", dirs, wantDirs)
	}
	if !reflect.DeepEqual(symlinks, wantSymlinks) {
		t.Errorf("symlinks=%q; want=%q", symlinks, wantSymlinks)
	}
	return dir
}

func readDir(dir *rpb.Directory) (files, dirs, symlinks []string) {
	for _, e := range dir.Files {
		files = append(files, e.Name)
	}
	for _, e := range dir.Directories {
		dirs = append(dirs, e.Name)
	}
	for _, e := range dir.Symlinks {
		symlinks = append(symlinks, e.Name)
	}
	return files, dirs, symlinks
}

// Given a directory `dir` and an entry `name`, returns:
// - Digest of entry within `dir` with name=`name`. For symlinks, returns empty digest.
// - Whether it is executable. For symlinks, returns false even if the symlink's target can be executable.
func getDigest(dir *rpb.Directory, name string) (*rpb.Digest, bool, error) {
	for _, e := range dir.Files {
		if e.Name == name {
			return e.Digest, e.IsExecutable, nil
		}
	}
	for _, e := range dir.Symlinks {
		if e.Name == name {
			return &rpb.Digest{}, false, nil
		}
	}
	for _, e := range dir.Directories {
		if e.Name == name {
			return e.Digest, false, nil
		}
	}
	return nil, false, errors.New("not found")
}

func openDir(ctx context.Context, ds *digest.Store, d *rpb.Digest) (*rpb.Directory, error) {
	data, ok := ds.Get(d)
	if !ok {
		return nil, fmt.Errorf("%v not found", d)
	}
	dir := &rpb.Directory{}
	err := datasource.ReadProto(ctx, data, dir)
	return dir, err
}
