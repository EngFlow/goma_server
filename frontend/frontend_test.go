// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package frontend

import "testing"

func TestParseUserAgent(t *testing.T) {
	for _, tc := range []struct {
		header   string
		wantHash string
		wantTime string
	}{
		{
			header:   "compiler-proxy built by chrome-bot at 7f258745e612a85dcc7896ccc09c22785ecccdb8@1539330943 on 2018-10-12T08:06:33.429908Z",
			wantHash: "7f258745e612a85dcc7896ccc09c22785ecccdb8",
			wantTime: "1539330943",
		},
	} {
		gotHash, gotTime, err := parseUserAgent(tc.header)
		if err != nil {
			t.Errorf("parseUserAgent(%q)=%q, %q, %v; want nil error", tc.header, gotHash, gotTime, err)
			continue
		}
		if gotHash != tc.wantHash || gotTime != tc.wantTime {
			t.Errorf("parseUserAgent(%q)=%q, %q, nil; want %q, %q, nil", tc.header, gotHash, gotTime, tc.wantHash, tc.wantTime)
		}

	}
}

func TestParseUserAgentError(t *testing.T) {
	for _, tc := range []string{
		"compiler-proxy built by chrome-bot at 19698870 on 2011-02-25T06:29:16.365771Z",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/69.0.3497.100 Safari/537.36",
	} {
		gotHash, gotTime, err := parseUserAgent(tc)
		if err == nil {
			t.Errorf("parseUserAgent(%q)=%q, %q, nil; want error", tc, gotHash, gotTime)
		}
	}
}
