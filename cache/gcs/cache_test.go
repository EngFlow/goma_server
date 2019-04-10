// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package gcs

import (
	"crypto/md5"
	"hash/crc32"
	"testing"
)

// hash value was retrieved from
//  gsutil ls -L gs://goma-dev-temp/empty
//  gsutil ls -L gs://goma-dev-temp/hello.c

func TestHash(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		in     string
		crc32c string
		md5    string
	}{
		{
			desc:   "empty",
			in:     "",
			crc32c: "AAAAAA==",
			md5:    "1B2M2Y8AsgTpgAmY7PhCfg==",
		},
		{
			desc: "hello.c",
			in: `#include <stdio.h>

int i = 1;

int main(int argc, char *argv[]) {
	 printf("hello, world\n");
	 return 0;
}
`,
			crc32c: "r+rffw==",
			md5:    "Q9yWleHH0r5N2AYXTIBHQw==",
		},
	} {
		got := crc32cStr(crc32.Checksum([]byte(tc.in), crc32cTable))
		if got != tc.crc32c {
			t.Errorf("%s: crc32c: %s; want %s", tc.desc, tc.in, tc.crc32c)
		}
		md5sum := md5.Sum([]byte(tc.in))
		got = md5sumStr(md5sum[:])
		if got != tc.md5 {
			t.Errorf("%s: md5: %s; want %s", tc.desc, tc.in, tc.md5)
		}
	}
}
