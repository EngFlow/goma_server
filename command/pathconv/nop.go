// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package pathconv

import (
	"strings"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
)

type nopPathConverter struct{}

// Default returns a path converter for the case server and client are both same posix path namesystem.
func Default() PathConverter {
	return nopPathConverter{}
}

func (nopPathConverter) ToClientPath(path string) (string, error) {
	return path, nil
}

func (nopPathConverter) ToServerPath(path string) (string, error) {
	return path, nil
}

func (nopPathConverter) IsAbsClient(path string) bool {
	return posixpath.IsAbs(path)
}

func (nopPathConverter) JoinClient(elem ...string) string {
	return posixpath.Join(elem...)
}

func (nopPathConverter) IsSafeClient(path string) bool {
	if !posixpath.IsAbs(path) {
		return false
	}
	elems := strings.Split(path, "/")
	depth := 0
	for _, elem := range elems[1:] {
		switch elem {
		case "", ".":
			continue
		case "..":
			depth--
			if depth < 0 {
				return false
			}
		default:
			depth++
		}
	}

	return true
}
