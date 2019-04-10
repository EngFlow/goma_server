// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package healthz provides /healthz for grpc server.
package healthz

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"go.chromium.org/goma/server/log"
)

var (
	connMu sync.Mutex
	conn   *grpc.ClientConn
)

func dialOnce(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	connMu.Lock()
	defer connMu.Unlock()
	if conn != nil {
		return conn, nil
	}
	logger := log.FromContext(ctx)
	deadline, ok := ctx.Deadline()
	if ok {
		logger.Debugf("dialing %s for /healthz check. deadline=%s", addr, deadline)
	} else {
		logger.Debugf("dialing %s for /healthz check", addr)
	}
	var err error
	conn, err = grpc.DialContext(ctx, addr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return conn, nil
}

var (
	mu        sync.Mutex
	unhealthy string
)

// SetUnhealthy sets m as unhealthy message.
// empty message means healthy.
func SetUnhealthy(m string) {
	mu.Lock()
	defer mu.Unlock()
	unhealthy = m
}

func getUnhealthy() string {
	mu.Lock()
	defer mu.Unlock()
	return unhealthy
}

// Register registers /healthz handler for grpc server.
func Register(s *grpc.Server, addr string) {
	healthpb.RegisterHealthServer(s, health.NewServer())
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := log.FromContext(ctx)
		now := time.Now()

		m := getUnhealthy()
		if m != "" {
			logger.Warnf("/healthz reports unhealthy: %s", m)
			http.Error(w, m, http.StatusServiceUnavailable)
			return
		}

		conn, err := dialOnce(ctx, addr)
		if err != nil {
			logger.Warnf("/healthz check failed for %s to dial: %v", time.Since(now), err)
			http.Error(w, fmt.Sprintf("failed to create grpc connection: %v", err), http.StatusServiceUnavailable)
			return
		}

		hc := healthpb.NewHealthClient(conn)
		resp, err := hc.Check(ctx, &healthpb.HealthCheckRequest{})
		if err != nil {
			logger.Errorf("/healthz check failed to call Check: %v", err)
			http.Error(w, fmt.Sprintf("failed to call Check: %v", err), http.StatusServiceUnavailable)
			return
		}
		if resp.Status != healthpb.HealthCheckResponse_SERVING {
			logger.Errorf("/healthz check failed to get serving status: %v", resp.Status)
			http.Error(w, fmt.Sprintf("health server is not serving: %v", resp.Status), http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
		logger.Debugf("%s is healthy: %s", addr, time.Since(now))
	})
}
