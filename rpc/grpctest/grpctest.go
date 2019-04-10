// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Package grpctest provides a test server for unit tests that use gRPC.

*/
package grpctest

import (
	"fmt"
	"net"

	"google.golang.org/grpc"
)

// StartServer instantiates a gRPC server suitable for unit tests, and
// returns the server address the client can use to connect and a stop
// function that must be called if err is nil to stop the server and
// cleanup resources.
func StartServer(server *grpc.Server) (addr string, stop func(), err error) {
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		return "", func() {}, fmt.Errorf("error creating TCP listener: %v", err)
	}
	addr = lis.Addr().String()
	go server.Serve(lis)
	stop = func() {
		server.Stop()
		lis.Close()
	}
	return addr, stop, nil
}
