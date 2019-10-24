// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package winpath handles windows-path (backslash separated path).
// It also accepts slash as path separator.
package winpath

import (
	"fmt"
	"regexp"
	"strings"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
)

var (
	absPathPattern = regexp.MustCompile(`^[A-Za-z]:[/\\].*`)
)

// FilePath provides win filepath.
type FilePath struct{}

func (FilePath) IsAbs(path string) bool     { return IsAbs(path) }
func (FilePath) Base(path string) string    { return Base(path) }
func (FilePath) Dir(path string) string     { return Dir(path) }
func (FilePath) Join(elem ...string) string { return Join(elem...) }

func (FilePath) Rel(basepath, targpath string) (string, error) {
	return Rel(basepath, targpath)
}

func (FilePath) Clean(path string) string       { return Clean(path) }
func (FilePath) SplitElem(path string) []string { return SplitElem(path) }
func (FilePath) PathSep() string                { return `\` }

// IsAbs returns true if fname is absolute path.
func IsAbs(fname string) bool {
	return absPathPattern.MatchString(fname)
}

// Base returns the last element of fname.
// If fname is empty, or ends with path separator, Base returns ".".
// If fname is `\` only, Base returns `\`.
func Base(fname string) string {
	if fname == "" {
		return "."
	}
	fname = fixPathSep(fname, '\\', '/')
	_, path := splitDrive(fname)
	if path == "" {
		return "."
	}
	base := posixpath.Base(path)
	return fixPathSep(base, '/', '\\')
}

// Dir returns all but the last element of path, typically the path's
// directory.  Differnt from filepath.Dir, it won't clean ".." in the result.
// If path is empty, Dir returns ".".
func Dir(fname string) string {
	if fname == "" {
		return "."
	}
	fname = fixPathSep(fname, '\\', '/')
	drive, path := splitDrive(fname)
	if path == "" {
		return drive
	}
	dirname := posixpath.Dir(path)
	return drive + fixPathSep(dirname, '/', '\\')
}

// Join joins any number of path elements into a single path, adding a "/"
// if necessary.  Different from filepath.Join, it won't clean ".." in
// the result. All empty strings are ignored.
func Join(elem ...string) string {
	if len(elem) == 0 {
		return ""
	}
	// copy
	elem = append([]string{}, elem...)
	for i := range elem {
		elem[i] = fixPathSep(elem[i], '\\', '/')
	}
	var drive string
	drive, elem[0] = splitDrive(elem[0])
	path := posixpath.Join(elem...)
	return drive + fixPathSep(path, '/', '\\')
}

// Rel returns a relative path that is lexically equivalent to targpath when
// joined to basepath with an intervening separator.
// TODO: case insensitive match.
func Rel(basepath, targpath string) (string, error) {
	if IsAbs(basepath) != IsAbs(targpath) {
		return "", fmt.Errorf("Rel: can't make %s relative to %s", targpath, basepath)
	}
	bdrive, bpath := splitDrive(basepath)
	tdrive, tpath := splitDrive(targpath)
	if bdrive != tdrive {
		return "", fmt.Errorf("Rel: can't make %s relative to %s", targpath, basepath)
	}
	bpath = fixPathSep(bpath, '\\', '/')
	tpath = fixPathSep(tpath, '\\', '/')
	rpath, err := posixpath.Rel(bpath, tpath)
	if err != nil {
		return "", err
	}
	return fixPathSep(rpath, '/', '\\'), nil
}

// Clean returns the shortest path name equivalent to path by purely lexical processing.
func Clean(path string) string {
	drive, path := splitDrive(path)
	path = fixPathSep(path, '\\', '/')
	path = posixpath.Clean(path)

	return drive + fixPathSep(path, '/', '\\')
}

// SplitElem splits path into element, separated by `\` or "/".
// If fname is absolute path, first element is `\` or `<drive>:\`,
// otherwise if fname has drive, first element is `<drive>:`.
// If fname ends with `\` or `\.`, last element is ".".
// Empty string, "/", `\` or "." won't be appeared in other elements.
func SplitElem(fname string) []string {
	drive, path := splitDrive(fname)
	path = fixPathSep(path, '\\', '/')
	elems := posixpath.SplitElem(path)
	if len(elems) > 0 && elems[0] == "/" {
		elems[0] = drive + `\`
	} else if drive != "" {
		elems = append([]string{drive}, elems...)
	}
	return elems
}

func fixPathSep(path string, oldSep, newSep byte) string {
	if !strings.ContainsRune(path, rune(oldSep)) {
		return path
	}
	buf := make([]byte, len(path))
	for i := 0; i < len(path); i++ {
		b := path[i]
		if b == oldSep {
			b = newSep
		}
		buf[i] = b
	}
	return string(buf)
}

func isDriveLetter(ch byte) bool {
	switch {
	case ch >= 'A' && ch <= 'Z':
		return true
	case ch >= 'a' && ch <= 'z':
		return true
	default:
		return false
	}
}

func splitDrive(fname string) (string, string) {
	if len(fname) == 0 {
		return "", ""
	}
	if !isDriveLetter(fname[0]) {
		return "", fname
	}
	if len(fname) < 2 {
		return "", fname
	}
	if fname[1] != ':' {
		return "", fname
	}
	// matches ^([A-Za-z]:)
	return fname[:2], fname[2:]
}
