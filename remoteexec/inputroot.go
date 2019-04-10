// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"errors"
	"fmt"
	"strings"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
	"go.chromium.org/goma/server/command/descriptor/winpath"
	gomapb "go.chromium.org/goma/server/proto/api"
)

func samePathElement(a, b []string) []string {
	for i, p := range a {
		if i >= len(b) {
			return a[:i]
		}
		if p != b[i] {
			return a[:i]
		}
	}
	return a
}

func commonDir(filepath clientFilePath, paths []string) string {
	if len(paths) == 0 {
		return "/"
	}
	path := filepath.SplitElem(paths[0])
	for _, p := range paths[1:] {
		path = samePathElement(path, filepath.SplitElem(p))
	}
	return filepath.Join(path...)
}

// argv0InInputRoot checks argv0 should be in input root or not.
// when argv0 is /usr/bin/*, it might use
// the command in platform container image, so not
// considered it as part of input root.
// TODO: non-linux platforms?
func argv0InInputRoot(argv0 string) bool {
	for _, dir := range []string{"/bin/", "/sbin/", "/usr/bin/", "/usr/sbin/"} {
		if strings.HasPrefix(argv0, dir) {
			return false
		}
	}
	return true
}

func inputPaths(filepath clientFilePath, req *gomapb.ExecReq, argv0 string) ([]string, error) {
	cwd := filepath.Clean(req.GetCwd())
	if !filepath.IsAbs(cwd) {
		return nil, fmt.Errorf("cwd is not abs path: %s", cwd)
	}
	paths := []string{cwd}
	for _, input := range req.Input {
		filename := input.GetFilename()
		if !filepath.IsAbs(filename) {
			filename = filepath.Join(cwd, filename)
		}
		paths = append(paths, filepath.Clean(filename))
	}
	if argv0InInputRoot(argv0) {
		if !filepath.IsAbs(argv0) {
			argv0 = filepath.Join(cwd, argv0)
		}
		paths = append(paths, filepath.Clean(argv0))
	}
	return paths, nil
}

func inputRootDir(filepath clientFilePath, paths []string) (string, error) {
	root := commonDir(filepath, paths)
	err := checkInputRootDir(filepath, root)
	if err != nil {
		return "", err
	}
	return root, nil
}

func checkInputRootDir(filepath clientFilePath, dir string) error {
	if dir == "" {
		return errors.New("no common paths in inputs")
	}
	switch filepath.(type) {
	case posixpath.FilePath:
		if dir == "/" {
			return errors.New("no common paths in inputs")
		}
		// if dir covers these paths, command (e.g. clang) won't
		// work because required *.so etc would not be accessible.
		for _, p := range []string{
			"/lib/x86_64-linux-gnu/",
			"/usr/lib/x86_64-linux-gnu/",
			"/lib64/",
		} {
			if strings.HasPrefix(p, dir+"/") {
				return fmt.Errorf("bad input root: %s", dir)
			}
		}
		return nil

	case winpath.FilePath:
		// TODO: check for win path.
		return nil

	default:
		// unknown filepath?
		return nil
	}
}

var errOutOfRoot = errors.New("out of root")

// rootRel returns relative path from rootDir for fname,
// which is relative path from cwd, or absolute path.
func rootRel(filepath clientFilePath, fname, cwd, rootDir string) (string, error) {
	if filepath == nil {
		return "", errors.New("rootRel: client filepath unknown")
	}
	if !filepath.IsAbs(fname) {
		fname = filepath.Join(filepath.Clean(cwd), fname)
	}
	// filepath.Rel cleans paths, so we can't use it here.
	// suppose rootdir and cwd are clean path, and
	// cwd is under rootdir.
	rootElems := filepath.SplitElem(filepath.Clean(rootDir))
	fileElems := filepath.SplitElem(fname)

	if len(fileElems) < len(rootElems) {
		return "", errOutOfRoot
	}
	for i, elem := range rootElems {
		if fileElems[i] != elem {
			return "", errOutOfRoot
		}
	}
	fileElems = fileElems[len(rootElems):]
	relname := filepath.Join(fileElems...)
	fileElems = filepath.SplitElem(filepath.Clean(relname))
	if len(fileElems) > 0 && fileElems[0] == ".." {
		return "", errOutOfRoot
	}
	return relname, nil
}
