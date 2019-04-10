// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package rpc

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/golang/groupcache/consistenthash"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"go.chromium.org/goma/server/log"
)

const (
	lookupInterval = 3 * time.Second
)

// backend represents each backend per IP.
type backend struct {
	addr string

	mu     sync.Mutex
	cc     *grpc.ClientConn
	client interface{} // proto's client interface
	nreq   int64
	nerr   int64
	load   int
	err    error
}

func (b *backend) init(ctx context.Context, target string, newc func(*grpc.ClientConn) interface{}, dialOpts []grpc.DialOption) error {
	span := trace.FromContext(ctx)
	span.AddAttributes(
		trace.StringAttribute("target", target),
		trace.StringAttribute("addr", b.addr),
	)
	logger := log.FromContext(ctx)

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != nil {
		return nil
	}
	logger.Infof("dial %s (for %s)", b.addr, target)
	cc, err := grpc.DialContext(ctx, b.addr, dialOpts...)
	if err != nil {
		logger.Errorf("dial %s (for %s) ... %v", b.addr, target, err)
		b.err = err
		return err
	}
	b.cc = cc
	b.client = newc(cc)
	return nil
}

func (b *backend) use() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load++
	b.nreq++
}

func (b *backend) done(ctx context.Context, err error) {
	logger := log.FromContext(ctx)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.load--
	if err != nil {
		b.nerr++
		logger.Warnf("backend error %s: %v", b.addr, err)
	}
}

func (b *backend) close() {
	if b == nil {
		return
	}
	if b.cc == nil {
		return
	}
	b.cc.Close()
	b.cc = nil
	b.client = nil
}

// Client represents load-balancing client.
type Client struct {
	target   string
	newc     func(cc *grpc.ClientConn) interface{}
	dialOpts []grpc.DialOption

	closech chan chan bool

	mu        sync.RWMutex
	timestamp time.Time
	addrs     []string
	backends  map[string]*backend
	shards    *consistenthash.Map
}

var (
	clientsMu sync.Mutex
	clients   []*Client
)

// Option configures Client.
type Option func(*Client)

// DialOption returns an Option to configure dial option.
func DialOption(opt grpc.DialOption) Option {
	return func(c *Client) {
		c.dialOpts = append(c.dialOpts, opt)
	}
}

// DialOptions converts grpc.DialOptions to Options.
func DialOptions(opts ...grpc.DialOption) []Option {
	var dopts []Option
	for _, opt := range opts {
		dopts = append(dopts, DialOption(opt))
	}
	return dopts
}

// NewClient creates new load-balancing client.
// newc should return grpc client interface for given *grpc.ClientConn.
// target is <hostname>:<port>.
// TODO: support grpc new naming?
func NewClient(ctx context.Context, target string, newc func(cc *grpc.ClientConn) interface{}, opts ...Option) *Client {
	client := &Client{
		target:  target,
		newc:    newc,
		closech: make(chan chan bool),
	}
	for _, opt := range opts {
		opt(client)
	}
	logger := log.FromContext(ctx)
	client.update(ctx)
	go func() {
		ctx := context.Background()
		logger := log.FromContext(ctx)
		for {
			select {
			case ch := <-client.closech:
				close(ch)
				logger.Debugf("finish client update for %s", target)
				return
			case <-time.After(lookupInterval):
			}
			logger := log.FromContext(ctx)
			if err := client.update(ctx); err != nil {
				logger.Errorf("rpc.Client.update(%s)=%v", target, err)
			}
		}
	}()
	clientsMu.Lock()
	clients = append(clients, client)
	logger.Debugf("add client %s", target)
	clientsMu.Unlock()
	return client
}

func (c *Client) update(ctx context.Context) error {
	span := trace.FromContext(ctx)
	span.AddAttributes(trace.StringAttribute("update.target", c.target))
	logger := log.FromContext(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.timestamp.Add(lookupInterval)) {
		return nil
	}
	logger.Debugf("lookup %s", c.target)
	host, port, err := net.SplitHostPort(c.target)
	if err != nil {
		logger.Errorf("lookup %s ... %v", c.target, err)
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		logger.Errorf("lookup %s ... %v", host, err)
		return err
	}
	c.timestamp = time.Now()

	c.addrs = make([]string, len(addrs))
	copy(c.addrs, addrs)
	sort.Strings(c.addrs)

	m := make(map[string]bool)
	for addr := range c.backends {
		m[addr] = true
	}
	// creates new backends from addrs
	backends := make(map[string]*backend)
	for _, addr := range addrs {
		b := &backend{
			addr: net.JoinHostPort(addr, port),
		}
		ob := c.backends[addr]
		if ob != nil {
			delete(m, addr)
			ob.mu.Lock()
			if ob.err == nil {
				b = ob
			} else if ob.cc != nil {
				ob.close()
			}
			ob.mu.Unlock()
		} else {
			m[addr] = true
		}
		backends[addr] = b
	}
	if len(m) == 0 {
		logger.Debug("address not changed")
		return nil
	}
	logger.Debugf("address updated: %v", m)

	for addr, ob := range c.backends {
		if _, ok := backends[addr]; !ok {
			ob.close()
		}
	}

	c.backends = backends
	c.shards = consistenthash.New(len(addrs), nil)
	c.shards.Add(addrs...)
	return nil
}

// Shard picks one backend from client's target to request the key, and returns
// selected backend.
func (c *Client) Shard(ctx context.Context, key interface{}) (*backend, error) {
	span := trace.FromContext(ctx)
	logger := log.FromContext(ctx)

	err := c.update(ctx)
	if err != nil {
		span.SetStatus(trace.Status{
			Code:    int32(codes.Aborted),
			Message: fmt.Sprintf("lookup failed: %v", err),
		})
		logger.Errorf("lookup failed: %v", err)
		return nil, grpc.Errorf(codes.Aborted, "rpc: %s lookup failed: %v", c.target, err)
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	keystr, _ := key.(string)
	addr := c.shards.Get(keystr)
	if addr == "" {
		span.SetStatus(trace.Status{
			Code:    int32(codes.Aborted),
			Message: "no backend",
		})
		logger.Errorf("no backend")
		return nil, grpc.Errorf(codes.Aborted, "rpc: %s no backends available", c.target)
	}
	span.Annotatef(nil, "shard %s (for %s) from %d", addr, key, len(c.backends))
	logger.Debugf("shard %s (for %s) from %d", addr, key, len(c.backends))
	b := c.backends[addr]
	err = b.init(ctx, c.target, c.newc, c.dialOpts)
	if err != nil {
		return nil, err
	}
	b.use()
	return b, nil
}

// Call calls new rpc call.
// picker and key will be used to pick backend.
// picker will be Client's Pick, or Shard.
// Pick will use key for backend addr, or empty for least loaded.
// Shard will use key for sharding.
// Rand will use key for *RandomState.
// f is called with grpc client inferface for selected backend.
func (c *Client) Call(ctx context.Context, picker func(context.Context, interface{}) (*backend, error), key interface{}, f func(interface{}) error) error {
	logger := log.FromContext(ctx)
	b, err := picker(ctx, key)
	if err != nil {
		return err
	}
	err = f(b.client)
	if err != nil {
		logger.Debugf("call %s done: %v", b.addr, err)
	} else {
		logger.Debugf("call %s done", b.addr)
	}
	b.done(ctx, err)
	return err
}

func (c *Client) Close() error {
	ch := make(chan bool)
	c.closech <- ch
	<-ch
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, b := range c.backends {
		b.close()
	}
	return nil
}
