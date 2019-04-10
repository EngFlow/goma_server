// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.
// +build ignore

// client.go is sample client of file server.
//
// $ go run client.go 'host:port' store 'path'
// $ go run client.go 'host:port' lookup 'hash' 'path'
package main

import (
	"context"
	"fmt"
	"os"

	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"

	"go.chromium.org/goma/server/file"
	"go.chromium.org/goma/server/log"
	filepb "go.chromium.org/goma/server/proto/file"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "usage: go run client.go 'target' store 'path'\n")
		fmt.Fprintf(os.Stderr, "       go run client.go 'target' lookup 'hash' 'path'\n")
		os.Exit(1)
	}

	ctx := context.Background()
	logger := log.FromContext(ctx)
	trace.SetDefaultSampler(trace.AlwaysSample())
	target := os.Args[1]

	conn, err := grpc.Dial(target, grpc.WithBlock(), grpc.WithInsecure(), grpc.WithStatsHandler(&ocgrpc.ClientHandler{}))
	if err != nil {
		logger.Fatal(err)
	}
	defer conn.Close()
	disk := file.Disk{
		Client: filepb.NewFileServiceClient(conn),
	}
	cmd := os.Args[2]
	switch cmd {
	case "store":
		fname := os.Args[3]
		spec := &file.BlobSpec{}
		_, err := disk.FromLocal(ctx, client, fname, spec)
		if err != nil {
			logger.Fatal(err)
		}
		fmt.Printf("%s: %s size=%d n=%d <= %s\n", spec.HashKey, spec.Blob.GetBlobType(), spec.Blob.GetFileSize(), len(spec.Blob.GetHashKey()), fname)

	case "lookup":
		if len(os.Args) < 5 {
			fmt.Fprintf(os.Stderr, "       go run client.go 'target' lookup 'hash' 'path'\n")
			os.Exit(1)
		}
		hashKey := os.Args[3]
		fname := os.Args[4]
		spec := &file.BlobSpec{
			HashKey: hashKey,
		}
		err := disk.ToLocal(ctx, spec, fname)
		fmt.Printf("%s: %s size=%d n=%d => %s\n", spec.HashKey, spec.Blob.GetBlobType(), spec.Blob.GetFileSize(), len(spec.Blob.GetHashKey(), fname))
	default:
		fmt.Fprintf(os.Stderr, "usage: go run client.go 'target' store 'path'\n")
		fmt.Fprintf(os.Stderr, "       go run client.go 'target' lookup 'hash' 'path'\n")
		os.Exit(1)
	}
}
