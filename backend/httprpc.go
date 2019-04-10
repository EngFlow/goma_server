// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"

	pb "go.chromium.org/goma/server/proto/backend"
)

// FromHTTPRPCBackend creates new httprpc backend from cfg.
func FromHTTPRPCBackend(ctx context.Context, cfg *pb.HttpRpcBackend) (HTTPRPC, func(), error) {
	target, err := url.Parse(cfg.Target)
	if err != nil {
		return HTTPRPC{}, func() {}, err
	}
	return NewHTTPRPC(target), func() {}, nil
}

// HTTPRPC is httprpc backend.
type HTTPRPC struct {
	proxy *httputil.ReverseProxy
}

// NewHTTPRPC creates httprpc backend proxies to target (scheme + host).
// target's path etc will be ignored.
func NewHTTPRPC(target *url.URL) HTTPRPC {
	return HTTPRPC{
		proxy: reverseProxy(target),
	}
}

func reverseProxy(target *url.URL) *httputil.ReverseProxy {
	// almost same as httputil.NewSingleHostReverseProxy, but
	//  - don't modify query.
	//  - rewrite Host header.
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = path.Join(target.Path, req.URL.Path)
			if _, ok := req.Header["User-Agent"]; !ok {
				// explicitly disable User-Agent so it's not set to default value
				req.Header.Set("User-Agent", "")
			}
			req.Host = target.Host
			req.Header.Set("Host", target.Host)
		},
		Transport: http.DefaultClient.Transport,
	}
}

// Ping forwards requests to target.
func (h HTTPRPC) Ping() http.Handler {
	return h.proxy
}

// Exec forwards requests to target.
func (h HTTPRPC) Exec() http.Handler {
	return h.proxy
}

// ByteStream forwards requests to target.
func (h HTTPRPC) ByteStream() http.Handler {
	return h.proxy
}

// StoreFile forwards requests to target.
func (h HTTPRPC) StoreFile() http.Handler {
	return h.proxy
}

// LookupFile forwards requests to target.
func (h HTTPRPC) LookupFile() http.Handler {
	return h.proxy
}

// Execlog forwards requests to target.
func (h HTTPRPC) Execlog() http.Handler {
	return h.proxy
}
