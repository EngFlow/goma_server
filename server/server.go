// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package server provides functions for goma servers.
package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/log/errorreporter"
	"go.chromium.org/goma/server/server/healthz"
)

// Server is interface to control server.
type Server interface {
	// ListenAndServe listens and then serve to handle requests on incoming
	// connections.
	ListenAndServe() error

	// Shutdown gracefully shuts down the server.
	Shutdown(context.Context) error
}

// GRPC represents grpc server.
type GRPC struct {
	*grpc.Server
	net.Listener
}

// ListenAndServe listens on Listener and handles requests with Server.
func (g GRPC) ListenAndServe() error {
	reflection.Register(g.Server)
	healthz.Register(g.Server, g.Listener.Addr().String())
	return g.Server.Serve(g.Listener)
}

// Shutdown gracefully shuts down the server.
func (g GRPC) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		g.Server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NewGRPC creates grpc server listening on port.
func NewGRPC(port int, opts ...grpc.ServerOption) (GRPC, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return GRPC{}, err
	}
	opts = append(opts,
		grpc.StatsHandler(&ocgrpc.ServerHandler{}),
		grpc.UnaryInterceptor(func() func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
			interceptor := log.GRPCUnaryServerInterceptor()
			if errorreporter.Enabled() {
				return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
					defer errorreporter.Do(nil, &err)
					return interceptor(ctx, req, info, handler)
				}
			}
			return interceptor
		}()))
	s := grpc.NewServer(opts...)
	return GRPC{Server: s, Listener: lis}, nil
}

// NewHTTP creates http server.
func NewHTTP(port int, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}
}

type httpsServer struct {
	*http.Server
	certFile, keyFile string
}

func (hs httpsServer) ListenAndServe() error {
	return hs.Server.ListenAndServeTLS(hs.certFile, hs.keyFile)
}

// NewHTTPS creates https server.
func NewHTTPS(hs *http.Server, certFile, keyFile string) Server {
	return httpsServer{Server: hs, certFile: certFile, keyFile: keyFile}
}

// Run runs servers.
// This is typically invoked as the last statement in the server's main function.
func Run(ctx context.Context, servers ...Server) {
	ctx, cancel := context.WithCancel(ctx)
	logger := log.FromContext(ctx)

	// TODO: enable zpages here.
	// zpages.Handle(http.DefaultServeMux, "/debug")
	for _, s := range servers {
		go func(s Server) {
			defer cancel()
			err := s.ListenAndServe()
			if err == http.ErrServerClosed || err == nil {
				logger.Infof("http server closed")
				return
			}
			logger.Errorf("serve error: %v", err)
		}(s)
	}
	// Wait SIGTERM from kubernetes.
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM)

	select {
	case <-ctx.Done():
	case sig := <-signalCh:
		logger.Infof("catch signal: %s", sig)
	}
	cancel()
	ctx = context.Background()
	var wg sync.WaitGroup
	for _, s := range servers {
		wg.Add(1)
		go func(s Server) {
			defer wg.Done()
			err := s.Shutdown(ctx)
			if err != nil {
				logger.Errorf("Shutdown server error: %v", err)
			}
		}(s)
	}
	wg.Wait()
	Flush()
	logger.Infof("server shutdown complete")
	logger.Sync()
	os.Exit(0)
}
