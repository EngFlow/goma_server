// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"fmt"
	"go.chromium.org/goma/server/server"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestHTTPRPCPing(t *testing.T) {
	defaultTransport := http.DefaultTransport
	defaultClient := http.DefaultClient
	defer func() {
		http.DefaultTransport = defaultTransport
		http.DefaultClient = defaultClient
	}()
	server.SetupHTTPClient()

	const pingResponse = "ok - ping response"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprint(w, pingResponse)
	}))
	defer s.Close()

	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	p := NewHTTPRPC(u)
	ps := httptest.NewServer(p.Ping())
	defer ps.Close()

	t.Logf("httprpc: %s -> %s", ps.URL, s.URL)
	resp, err := http.Get(ps.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("resp=%d; want=200", resp.StatusCode)
	}
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
	}
	if string(buf) != pingResponse {
		t.Errorf("response=%q; want=%q", string(buf), pingResponse)
	}
}
