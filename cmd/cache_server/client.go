// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.
// +build ignore

// client.go is sample client of cache server.
//
//  $ go run client.go 'host:port' get 'key'
//  $ go run client.go 'host:port' put 'key' < value
package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"

	"go.chromium.org/goma/server/cache"
	pb "go.chromium.org/goma/server/proto/cache"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "usage: go run client.go 'target' [get|put] 'key'\n")
		os.Exit(1)
	}

	trace.SetDefaultSampler(trace.AlwaysSample())

	target := os.Args[1]
	client := cache.NewClient(target, grpc.WithInsecure(), grpc.WithStatsHandler(&ocgrpc.ClientHandler{}))
	cmd := os.Args[2]
	key := os.Args[3]
	ctx := context.Background()
	switch cmd {
	case "get":
		resp, err := client.Get(ctx, &pb.GetReq{
			Key: key,
		})
		if err != nil {
			log.Fatalf("get error: %v", err)
		}
		if len(resp.Kv.Value) == 0 {
			os.Exit(0)
		}
		_, err = os.Stdout.Write(resp.Kv.Value)
		if err != nil {
			log.Fatalf("write failed: %v", err)
		}
	case "put":
		value, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("read failed: %v", err)
		}
		_, err = client.Put(ctx, &pb.PutReq{
			Kv: &pb.KV{
				Key:   key,
				Value: value,
			},
		})
		if err != nil {
			log.Fatalf("put error: %v", err)
		}
	}
}
