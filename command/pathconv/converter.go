// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package pathconv

// PathConverter provides path conversion between a client path and a server path.
// It also provides client path utilities.
type PathConverter interface {
	// ToClientPath converts a server path to a client path.
	ToClientPath(string) (string, error)

	// ToServerPath converts an absolute client path to a server path.
	ToServerPath(string) (string, error)

	// IsAbsClient reports whether the path is absolute as client path.
	IsAbsClient(string) bool

	// JoinClient joins any number of path elements into a single path.
	JoinClient(elem ...string) string

	// IsSafeClient reports whether the path is safe absolute path
	// (not go out from root directory).
	// It returns false for relative path.
	IsSafeClient(string) bool
}
