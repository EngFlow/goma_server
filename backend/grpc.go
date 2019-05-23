// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	bspb "google.golang.org/genproto/googleapis/bytestream"

	"go.chromium.org/goma/server/httprpc"
	bytestreamrpc "go.chromium.org/goma/server/httprpc/bytestream"
	execrpc "go.chromium.org/goma/server/httprpc/exec"
	execlogrpc "go.chromium.org/goma/server/httprpc/execlog"
	filerpc "go.chromium.org/goma/server/httprpc/file"
	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/rpc"
)

// Auth authenticates the request.
type Auth interface {
	// Auth checks HTTP access, and returns new context with enduser info.
	Auth(context.Context, *http.Request) (context.Context, error)
}

// GRPC is grpc backend in the same cluster (local) or other cluter (remote).
type GRPC struct {
	ExecServer
	FileServer
	ExeclogServer

	ByteStreamClient bspb.ByteStreamClient

	Auth Auth
	// api key. used for remote backend.
	APIKey string

	// trace prefix and label. used for local backend.
	Namespace string
	Cluster   string
}

func (g GRPC) httprpcOpts(timeout time.Duration) []httprpc.HandlerOption {
	return []httprpc.HandlerOption{
		httprpc.Timeout(timeout),
		httprpc.WithRetry(rpc.Retry{}),
		httprpc.WithAuth(g.Auth),
		httprpc.WithAPIKey(g.APIKey),
		httprpc.WithNamespace(g.Namespace),
		httprpc.WithCluster(g.Cluster),
	}
}

// Ping returns http handler for ping.
func (g GRPC) Ping() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// TODO: hard fail if g.Auth == nil?
		if g.Auth != nil {
			ctx, err := g.Auth.Auth(req.Context(), req)
			if err != nil {
				code := http.StatusUnauthorized
				logger := log.FromContext(ctx)
				logger.Errorf("server error %s: %d %s: %v", req.URL.Path, code, http.StatusText(code), err)
				http.Error(w, http.StatusText(code), code)
				return
			}
			req = req.WithContext(ctx)
		}
		// Accept-Encoding: deflate only if client didn't say gzip,
		// since old goma client only recognizes "Accept-Encoding: deflate".
		// TODO: always accept gzip, deflate once new goma client released.
		if strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Accept-Encoding", "gzip, deflate")
		} else {
			w.Header().Set("Accept-Encoding", "deflate")
		}
		// TODO: health status of backend servers?
		fmt.Fprintln(w, "ok")
	})
}

// Exec returns http handler for exec request.
func (g GRPC) Exec() http.Handler {
	return execrpc.Handler(g.ExecServer, g.httprpcOpts(9*time.Minute+50*time.Second)...)
}

// ByteStream returns http handler for bytestream.
func (g GRPC) ByteStream() http.Handler {
	if g.ByteStreamClient == nil {
		return http.HandlerFunc(http.NotFound)
	}
	return bytestreamrpc.Handler(g.ByteStreamClient, g.httprpcOpts(1*time.Minute)...)
}

// StoreFile returns http handler for store file request.
func (g GRPC) StoreFile() http.Handler {
	return filerpc.StoreHandler(g.FileServer, g.httprpcOpts(1*time.Minute)...)
}

// LookupFile returns http handler for lookup file request.
func (g GRPC) LookupFile() http.Handler {
	return filerpc.LookupHandler(g.FileServer, g.httprpcOpts(1*time.Minute)...)
}

// Execlog returns http handler for execlog request.
func (g GRPC) Execlog() http.Handler {
	return execlogrpc.Handler(g.ExeclogServer, g.httprpcOpts(1*time.Minute)...)
}
