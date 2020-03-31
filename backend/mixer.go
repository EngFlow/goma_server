// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"go.chromium.org/goma/server/auth"
	"go.chromium.org/goma/server/auth/enduser"
	"go.chromium.org/goma/server/log"
	pb "go.chromium.org/goma/server/proto/backend"
)

func fromBackendMapping(ctx context.Context, cfg *pb.BackendMapping, opt Option) (Backend, func(), error) {
	groupId := cfg.GroupId
	if groupId == "" {
		groupId = "default group"
	}
	switch be := cfg.Backend.(type) {
	case *pb.BackendMapping_HttpRpc:
		return FromHTTPRPCBackend(ctx, be.HttpRpc)
	case *pb.BackendMapping_Remote:
		return FromRemoteBackend(ctx, be.Remote, opt)
	case nil:
		return nil, func() {}, fmt.Errorf("no backend for %s", groupId)
	default:
		return nil, func() {}, fmt.Errorf("unknown type in %s: %T", groupId, cfg.Backend)
	}
}

// FromBackendRule creates new Mixer from cfg.
// returned func would release resources associated with Mixer.
func FromBackendRule(ctx context.Context, cfg *pb.BackendRule, opt Option) (mixer Mixer, cleanup func(), err error) {
	var cleanups []func()
	defer func() {
		if err != nil {
			for _, c := range cleanups {
				c()
			}
		}
	}()
	logger := log.FromContext(ctx)
	mixer.Auth = opt.Auth
	mixer.backends = make(map[string]Backend)
	for _, backend := range cfg.Backends {
		if backend.GroupId == "" {
			if len(backend.QueryParams) > 0 {
				logger.Warnf("non empty query_params for default backend: %v", backend.QueryParams)
			}
			if mixer.defaultBackend != nil {
				return Mixer{}, func() {}, errors.New("duplicate default backend")
			}
			be, cleanup, err := fromBackendMapping(ctx, backend, opt)
			if err != nil {
				logger.Warnf("ignore bad backend[default] %s: %v", backend, err)
				continue
			}
			mixer.defaultBackend = be
			cleanups = append(cleanups, cleanup)
			continue
		}
		key := backendKeyFromProto(ctx, backend)
		if _, found := mixer.backends[key]; found {
			return Mixer{}, func() {}, fmt.Errorf("duplicate backend group: %s", key)
		}
		be, cleanup, err := fromBackendMapping(ctx, backend, opt)
		if err != nil {
			logger.Warnf("ignore bad backend %s: %v", backend, err)
			continue
		}
		mixer.backends[key] = be
		cleanups = append(cleanups, cleanup)
	}
	if len(mixer.backends) == 0 && mixer.defaultBackend == nil {
		return Mixer{}, func() {}, fmt.Errorf("no valid backends in %s", cfg)
	}
	return mixer, func() {
		for _, c := range cleanups {
			c()
		}
	}, nil
}

func backendKeyFromProto(ctx context.Context, m *pb.BackendMapping) string {
	q, err := url.ParseQuery(m.QueryParams)
	if err != nil {
		logger := log.FromContext(ctx)
		logger.Errorf("invalid query_params %q: %v", m.QueryParams, err)
		return backendKey(m.GroupId, nil)
	}
	return backendKey(m.GroupId, q)
}

func backendKey(group string, q url.Values) string {
	if len(q) == 0 {
		return group
	}
	return group + "?" + q.Encode()
}

// Mixer is mixer backend, dispatched by group of enduser.
type Mixer struct {
	backends       map[string]Backend
	defaultBackend Backend
	Auth           Auth
}

func (m Mixer) Ping() http.Handler       { return m.dispatcher(Backend.Ping) }
func (m Mixer) Exec() http.Handler       { return m.dispatcher(Backend.Exec) }
func (m Mixer) ByteStream() http.Handler { return m.dispatcher(Backend.ByteStream) }
func (m Mixer) StoreFile() http.Handler  { return m.dispatcher(Backend.StoreFile) }
func (m Mixer) LookupFile() http.Handler { return m.dispatcher(Backend.LookupFile) }
func (m Mixer) Execlog() http.Handler    { return m.dispatcher(Backend.Execlog) }

func (m Mixer) selectBackend(ctx context.Context, group string, q url.Values) (Backend, bool) {
	logger := log.FromContext(ctx)
	key := backendKey(group, q)
	backend, found := m.backends[key]
	if found {
		logger.Infof("backend %s", key)
		return backend, true
	}
	key = backendKey(group, nil)
	backend, found = m.backends[key]
	if found {
		logger.Infof("backend %s (ignore query param:%s)", key, q)
		return backend, true
	}
	backend = m.defaultBackend
	if backend != nil {
		logger.Infof("backend default for %s", key)
		return backend, true
	}
	return nil, false
}

func (m Mixer) dispatcher(handler func(Backend) http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		logger := log.FromContext(ctx)
		if m.Auth != nil {
			var err error
			ctx, err = m.Auth.Auth(ctx, req)
			if err != nil {
				code := http.StatusUnauthorized
				// Making the client to retry with the refreshed token instead of throttling.
				// Please see (b/150189886) for details.
				if err == auth.ErrExpired {
					code = http.StatusServiceUnavailable
				}
				http.Error(w, "The request requires user authentication", code)
				logger.Errorf("auth error %s: %d %s: %v", req.URL.Path, code, http.StatusText(code), err)
				return
			}
		}
		user, ok := enduser.FromContext(ctx)
		if !ok {
			code := http.StatusInternalServerError
			http.Error(w, "no enduser info available", code)
			logger.Errorf("server error %s: %d %s: no enduser in context", req.URL.Path, code, http.StatusText(code))
			return
		}
		q := req.URL.Query()
		backend, found := m.selectBackend(ctx, user.Group, q)
		if !found {
			logger.Errorf("no backend config for %s %s", user.Group, q.Encode())
			http.Error(w, "no backend config", http.StatusInternalServerError)
			return
		}
		h := handler(backend)
		h.ServeHTTP(w, req)
	})
}
