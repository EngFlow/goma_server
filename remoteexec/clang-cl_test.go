// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"reflect"
	"testing"
)

func TestClangclOutputs(t *testing.T) {
	for _, tc := range []struct {
		desc string
		args []string
		want []string
	}{
		{
			desc: "basic",
			args: []string{
				"clang-cl", "/c", "A/test.c",
				"/I", "A/B/C",
				"/ID/E/F",
				"/o", "A/test.o",
			},
			want: []string{"A/test.o"},
		},
		{
			desc: "basic /Fo /Fd",
			args: []string{
				"clang-cl", "/c", "A/test.c",
				"/I", "A/B/C",
				"/ID/E/F",
				"/FoA/test.o",
				"/FdA/test.pdb",
			},
			// TODO: capture pdb.
			want: []string{"A/test.o"},
		},
		{
			desc: "basic dash",
			args: []string{
				"clang-cl", "-c", "A/test.c",
				"-I", "A/B/C",
				"-ID/E/F",
				"-o", "A/test.o",
			},
			want: []string{"A/test.o"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if got := clangclOutputs(tc.args); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("clangclOutputs(%q)=%q; want %q", tc.args, got, tc.want)
			}
		})
	}
}
