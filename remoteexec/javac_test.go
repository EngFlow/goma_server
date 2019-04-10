// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"reflect"
	"testing"
)

func TestJavacOutputDirs(t *testing.T) {
	for _, tc := range []struct {
		desc string
		args []string
		want []string
	}{
		{
			desc: "basic",
			args: []string{
				"javac", "-J-Xmx512M",
				"-target", "1.8",
				"-d", "A",
				"-s", "B",
				"-cp", "foo.jar:bar.jar",
				"-bootclasspath", "baz.jar",
				"Hello.java", "World.java",
			},
			want: []string{"A", "B"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if got := javacOutputDirs(tc.args); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("clangclOutputs(%q)=%q; want %q", tc.args, got, tc.want)
			}
		})
	}
}
