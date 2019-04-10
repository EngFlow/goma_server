// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	pb "go.chromium.org/goma/server/proto/backend"
)

// Backend is interface of backend for frontend.
// TODO: provides switching backend,
// httprpc backend (e.g. to clients5.google.com/cxx-compiler-service/*) and
// remote backend (e.g. to <cluster>.endpoints.<project>.cloud.goog)
type Backend interface {
	Ping() http.Handler
	Exec() http.Handler
	ByteStream() http.Handler
	StoreFile() http.Handler
	LookupFile() http.Handler
	Execlog() http.Handler
}

// Option is backend option.
type Option struct {
	Auth      Auth
	APIKeyDir string
}

// FromProto creates Backend based on cfg.
// returned func will release resources associated with Backend.
func FromProto(ctx context.Context, cfg *pb.BackendConfig, opt Option) (Backend, func(), error) {
	switch be := cfg.Backend.(type) {
	case *pb.BackendConfig_Local:
		return FromLocalBackend(ctx, be.Local, opt)
	case *pb.BackendConfig_HttpRpc:
		return FromHTTPRPCBackend(ctx, be.HttpRpc)
	case *pb.BackendConfig_Remote:
		return FromRemoteBackend(ctx, be.Remote, opt)
	case *pb.BackendConfig_Rule:
		return FromBackendRule(ctx, be.Rule, opt)
	case nil:
		return nil, func() {}, errors.New("no backend in config")
	default:
		return nil, func() {}, fmt.Errorf("unknown type in backend: %T", cfg.Backend)
	}
}
