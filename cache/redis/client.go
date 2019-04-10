// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package redis

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"

	"github.com/gomodule/redigo/redis"
	"google.golang.org/grpc"

	pb "go.chromium.org/goma/server/proto/cache"
	"go.chromium.org/goma/server/rpc"
)

// Client is cache service client for redis.
type Client struct {
	prefix string
	pool   *redis.Pool
}

// AddrFromEnv returns redis server address from environment variables.
func AddrFromEnv() (string, error) {
	host := os.Getenv("REDISHOST")
	port := os.Getenv("REDISPORT")
	if host == "" {
		return "", fmt.Errorf("no REDISHOST environment")
	}
	if port == "" {
		port = "6379" // redis default port
	}
	return fmt.Sprintf("%s:%s", host, port), nil
}

// NewClient creates new cache client for redis.
func NewClient(ctx context.Context, addr, prefix string) Client {
	return Client{
		prefix: prefix,
		pool: &redis.Pool{
			Dial: func() (redis.Conn, error) {
				return redis.Dial("tcp", addr)
			},
			MaxIdle:   10,
			MaxActive: 200,
			Wait:      true,
		},
	}
}

// Close releases the resources used by the client.
func (c Client) Close() error {
	return c.pool.Close()
}

type temporary interface {
	Temporary() bool
}

func isConnError(err error) bool {
	errno, ok := err.(syscall.Errno)
	if !ok {
		serr, ok := err.(*os.SyscallError)
		if !ok {
			return false
		}
		errno, ok = serr.Err.(syscall.Errno)
		if !ok {
			return false
		}
	}
	return errno == syscall.ECONNRESET || errno == syscall.ECONNABORTED
}

// retryErr converts err to rpc.RetriableError if it is retriable error.
func retryErr(err error) error {
	// retriable if temporary error.
	if terr, ok := err.(temporary); ok && terr.Temporary() {
		return rpc.RetriableError{
			Err: err,
		}
	}
	// redis might return net.OpError as is.
	operr, ok := err.(*net.OpError)
	if !ok {
		return err
	}
	// retry if it is ECONNRESET or ECONNABORTED.
	if isConnError(operr.Err) {
		return rpc.RetriableError{
			Err: err,
		}
	}
	return err
}

// Get fetches value for the key from redis.
func (c Client) Get(ctx context.Context, in *pb.GetReq, opts ...grpc.CallOption) (*pb.GetResp, error) {
	conn, err := c.pool.GetContext(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var v []byte
	err = rpc.Retry{
		MaxRetry: -1,
	}.Do(ctx, func() error {
		v, err = redis.Bytes(conn.Do("GET", c.prefix+in.Key))
		return retryErr(err)
	})
	if err != nil {
		return nil, err
	}
	return &pb.GetResp{
		Kv: &pb.KV{
			Key:   in.Key,
			Value: v,
		},
		InMemory: true,
	}, nil
}

// Put stores key:value pair on redis.
func (c Client) Put(ctx context.Context, in *pb.PutReq, opts ...grpc.CallOption) (*pb.PutResp, error) {
	conn, err := c.pool.GetContext(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	err = rpc.Retry{
		MaxRetry: -1,
	}.Do(ctx, func() error {
		_, err := conn.Do("SET", c.prefix+in.Kv.Key, in.Kv.Value)
		return retryErr(err)
	})
	if err != nil {
		return nil, err
	}
	return &pb.PutResp{}, nil
}
