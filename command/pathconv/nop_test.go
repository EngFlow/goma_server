// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package pathconv

import (
	"testing"
)

func TestPathConverter(t *testing.T) {
	testcases := []struct {
		clientPath string
		serverPath string
	}{
		{
			clientPath: "",
			serverPath: "",
		},
		{
			clientPath: "/tmp/foo.c",
			serverPath: "/tmp/foo.c",
		},
	}

	c := Default()
	for _, tc := range testcases {
		got, err := c.ToServerPath(tc.clientPath)
		if err != nil {
			t.Errorf("ToServerPath(%q)=_,%v; want nil", tc.clientPath, err)
		}
		if got != tc.serverPath {
			t.Errorf("ToServerPath(%q)=%q,_; want %q", tc.clientPath, got, tc.serverPath)
		}
	}

	for _, tc := range testcases {
		got, err := c.ToClientPath(tc.serverPath)
		if err != nil {
			t.Errorf("ToClientPath(%q)=_,%v; want nil", tc.serverPath, err)
		}
		if got != tc.serverPath {
			t.Errorf("ToClientPath(%q)=%q,_; want %q", tc.serverPath, got, tc.clientPath)
		}
	}
}

func TestIsSafeClient(t *testing.T) {
	testcases := []struct {
		path string
		safe bool
	}{
		{
			path: "",
			safe: false,
		},
		{
			path: "/",
			safe: true,
		},
		{
			path: "a",
			safe: false,
		},
		{
			path: "a/b",
			safe: false,
		},
		{
			path: "/b",
			safe: true,
		},
		{
			path: "/a/b",
			safe: true,
		},
		{
			path: "a/b/c",
			safe: false,
		},
		{
			path: "a/../../b/./..",
			safe: false,
		},
		{
			path: "/../../",
			safe: false,
		},
		{
			path: "/../../a/b/",
			safe: false,
		},
		{
			path: "/a/../..",
			safe: false,
		},
	}

	c := Default()
	for _, tc := range testcases {
		got := c.IsSafeClient(tc.path)
		if got != tc.safe {
			t.Errorf("IsSafeClient(%q)=%t; want %t", tc.path, got, tc.safe)
		}
	}
}
