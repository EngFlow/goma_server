// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/cache"
	"go.chromium.org/goma/server/file"
	"go.chromium.org/goma/server/hash"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	cachepb "go.chromium.org/goma/server/proto/cache"
	cpb "go.chromium.org/goma/server/proto/command"
	fpb "go.chromium.org/goma/server/proto/file"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/rpc/grpctest"
)

// fakeCluster represents fake goma cluster.
type fakeCluster struct {
	rbe  *fakeRBE
	srv  *grpc.Server
	addr string
	conn *grpc.ClientConn
	stop func()

	cache *cache.Cache
	csrv  *grpc.Server
	cconn *grpc.ClientConn
	cstop func()

	gomafile *file.Service
	fsrv     *grpc.Server
	fconn    *grpc.ClientConn
	fstop    func()

	cmdStorage fakeCmdStorage
	redis      fakeRedis

	adapter Adapter
}

// setup sets up fake goma cluster with fake RBE instance.
func (f *fakeCluster) setup(ctx context.Context, instancePrefix string) error {
	var err error
	var defers []func()
	defer func() {
		for i := len(defers) - 1; i >= 0; i-- {
			defers[i]()
		}
	}()

	logger := log.FromContext(ctx)

	// CAS BatchUpdateBlobs will accept at most max_batch_total_size_bytes.
	// https://github.com/bazelbuild/remote-apis/blob/efd28d1832bd3ccddc3d2b29c341da1cce09c333/build/bazel/remote/execution/v2/remote_execution.proto#L1278
	// TODO: set max msg size to match with max_batch_total_size_bytes
	// in ServerCapabilities.CacheCapabitilies.
	f.srv = grpc.NewServer()
	registerFakeRBE(f.srv, f.rbe)
	f.addr, f.stop, err = grpctest.StartServer(f.srv)
	if err != nil {
		return err
	}
	defers = append(defers, f.stop)
	logger.Infof("f.conn = addr:%s", f.addr)
	// TODO: make it secure and pass enduser credentials
	f.conn, err = grpc.Dial(f.addr, grpc.WithInsecure())
	if err != nil {
		return err
	}
	defers = append(defers, func() { f.conn.Close() })

	f.csrv = grpc.NewServer()
	f.cache, err = cache.New(cache.Config{
		MaxBytes: 1 * 1024 * 1024 * 1024,
	})
	if err != nil {
		return err
	}
	cachepb.RegisterCacheServiceServer(f.csrv, f.cache)
	var addr string
	addr, f.cstop, err = grpctest.StartServer(f.csrv)
	if err != nil {
		return err
	}
	defers = append(defers, f.cstop)
	logger.Infof("f.cconn = addr:%s", addr)
	f.cconn, err = grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return err
	}
	defers = append(defers, func() { f.cconn.Close() })
	f.fsrv = grpc.NewServer(grpc.MaxSendMsgSize(file.DefaultMaxMsgSize), grpc.MaxRecvMsgSize(file.DefaultMaxMsgSize))
	f.gomafile = &file.Service{
		Cache: cachepb.NewCacheServiceClient(f.cconn),
	}
	fpb.RegisterFileServiceServer(f.fsrv, f.gomafile)
	addr, f.fstop, err = grpctest.StartServer(f.fsrv)
	if err != nil {
		return err
	}
	defers = append(defers, f.fstop)
	logger.Infof("f.fconn = addr:%s", addr)
	f.fconn, err = grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return err
	}
	defers = append(defers, func() { f.fconn.Close() })

	f.adapter = Adapter{
		InstancePrefix:    instancePrefix,
		ExecTimeout:       10 * time.Second,
		Client:            Client{ClientConn: f.conn},
		GomaFile:          fpb.NewFileServiceClient(f.fconn),
		DigestCache:       digest.NewCache(&f.redis, 1e6),
		CmdStorage:        &f.cmdStorage,
		ToolDetails:       &rpb.ToolDetails{},
		FileLookupSema:    make(chan struct{}, 2),
		CASBlobLookupSema: make(chan struct{}, 2),
	}

	defers = nil
	return nil
}

// teardown cleans up fake cluster.
func (f *fakeCluster) teardown() {
	if f.fconn != nil {
		f.fconn.Close()
	}
	if f.fstop != nil {
		f.fstop()
	}
	if f.cconn != nil {
		f.cconn.Close()
	}
	if f.cstop != nil {
		f.cstop()
	}
}

// fakeToolchain represents fake toolchain.
type fakeToolchain struct {
	descs              []*cpb.CmdDescriptor
	RemoteexecPlatform *cpb.RemoteexecPlatform
}

// CommandSpec returns command spec for name and localPath.
func (f *fakeToolchain) CommandSpec(name, localPath string) *gomapb.CommandSpec {
	for _, desc := range f.descs {
		if desc.Selector.Name == name {
			return &gomapb.CommandSpec{
				Name:              proto.String(desc.Selector.Name),
				Version:           proto.String(desc.Selector.Version),
				Target:            proto.String(desc.Selector.Target),
				BinaryHash:        []byte(desc.Selector.BinaryHash),
				LocalCompilerPath: proto.String(localPath),
			}
		}
	}
	return &gomapb.CommandSpec{}
}

// newFakeClang creates new fake toolchain for clang version+target.
func newFakeClang(f *fakeCmdStorage, version, target string) *fakeToolchain {
	clangFile := f.newFileSpec("bin/clang", true)
	libFindBadConstructs := f.newFileSpec("lib/libFindBadConstructs.so", false)
	return &fakeToolchain{
		descs: []*cpb.CmdDescriptor{
			{
				Selector: &cpb.Selector{
					Name:       "clang",
					Version:    version,
					Target:     target,
					BinaryHash: clangFile.Hash,
				},
				Setup: &cpb.CmdDescriptor_Setup{
					CmdFile:  clangFile,
					PathType: cpb.CmdDescriptor_POSIX,
				},
			},
			{
				Selector: &cpb.Selector{
					Name:       "clang++",
					Version:    version,
					Target:     target,
					BinaryHash: clangFile.Hash,
				},
				Setup: &cpb.CmdDescriptor_Setup{
					CmdFile:  clangFile,
					PathType: cpb.CmdDescriptor_POSIX,
				},
			},
			{
				Selector: &cpb.Selector{
					Name:       "libFindBadConstructs.so",
					BinaryHash: libFindBadConstructs.Hash,
				},
				Setup: &cpb.CmdDescriptor_Setup{
					CmdFile:  libFindBadConstructs,
					PathType: cpb.CmdDescriptor_POSIX,
				},
			},
		},
		RemoteexecPlatform: &cpb.RemoteexecPlatform{
			Properties: []*cpb.RemoteexecPlatform_Property{
				{
					Name:  "container-image",
					Value: "docker://grpc.io/goma-dev/container-image@sha256:xxxx",
				},
			},
		},
	}
}

// pushToolchains push fake toolchain in fake cluster.
func (f *fakeCluster) pushToolchains(ctx context.Context, tc *fakeToolchain) error {
	config := &cpb.ConfigResp{
		VersionId: time.Now().String(),
	}
	for _, desc := range tc.descs {
		config.Configs = append(config.Configs, &cpb.Config{
			Target: &cpb.Target{
				Addr: f.addr,
			},
			BuildInfo:          &cpb.BuildInfo{},
			CmdDescriptor:      desc,
			RemoteexecPlatform: tc.RemoteexecPlatform,
		})
	}
	err := f.adapter.Inventory.Configure(ctx, config)
	return err
}

// pushPlatform pushes a platform with dimensions.
func (f *fakeCluster) pushPlatform(ctx context.Context, containerImage string, dimensions []string) error {
	config := &cpb.ConfigResp{
		VersionId: time.Now().String(),
	}

	config.Configs = []*cpb.Config{
		{
			RemoteexecPlatform: &cpb.RemoteexecPlatform{
				Properties: []*cpb.RemoteexecPlatform_Property{
					{
						Name:  "container-image",
						Value: containerImage,
					},
				},
			},
			Dimensions: dimensions,
		},
	}
	err := f.adapter.Inventory.Configure(ctx, config)
	return err
}

// fakeLocalFiles represents fake local files (in client side).
type fakeLocalFiles struct {
	m map[string]string // path -> content
}

func randomSize() uint64 {
	s := rand.Uint64() % (2 * 1024 * 1024)
	if s == 0 {
		s++
	}
	return s
}

func randomBigSize() uint64 {
	return 2*1024*1024 + randomSize()
}

// Add adds new fake file named fname.
func (f *fakeLocalFiles) Add(fname string, size uint64) {
	if f.m == nil {
		f.m = make(map[string]string)
	}
	buf := make([]byte, size)
	rand.Read(buf)
	f.m[fname] = string(buf)
}

// Dup dups oldname as newname.
func (f *fakeLocalFiles) Dup(oldname, newname string) {
	f.m[newname] = f.m[oldname]
}

// Open opens fake file.
func (f *fakeLocalFiles) Open(fname string) (io.Reader, error) {
	data, ok := f.m[fname]
	if !ok {
		return nil, fmt.Errorf("%s not found", fname)
	}
	return strings.NewReader(data), nil
}

// mustFileHash returns SHA256 hash of file content.
func (f *fakeLocalFiles) mustFileHash(ctx context.Context, t *testing.T, fname string) string {
	data, ok := f.m[fname]
	if !ok {
		t.Fatalf("%s not found", fname)
	}

	return hash.SHA256Content([]byte(data))
}

// mustDigest returns digest of file content.
func (f *fakeLocalFiles) mustDigest(ctx context.Context, t *testing.T, fname string) *rpb.Digest {
	data, ok := f.m[fname]
	if !ok {
		t.Fatalf("%s not found", fname)
	}
	return &rpb.Digest{
		Hash:      hash.SHA256Content([]byte(data)),
		SizeBytes: int64(len(data)),
	}
}

// mustInput creates execreq input for fname (maybe relative) from fullpath.
// if fc is nil, content won't be uploaded and not set in input (i.e. hash only).
func (f *fakeLocalFiles) mustInput(ctx context.Context, t *testing.T, fc fpb.FileServiceClient, fullpath, fname string) *gomapb.ExecReq_Input {
	data := f.m[fullpath]
	input := &gomapb.ExecReq_Input{
		Filename: proto.String(fname),
		Content: &gomapb.FileBlob{
			FileSize: proto.Int64(int64(len(data))),
		},
	}
	err := file.FromReader(ctx, fc, strings.NewReader(data), input.Content)
	if err != nil {
		t.Fatalf("failed to read file %s: %v", fullpath, err)
	}
	hashKey, err := hash.SHA256Proto(input.Content)
	if err != nil {
		t.Fatalf("failed to compute sha256 %s: %v", fullpath, err)
	}
	input.HashKey = proto.String(hashKey)
	if fc == nil {
		input.Content = nil
	}
	return input
}

// fakeRedis represents cache client on fake memorystore.
type fakeRedis struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (f *fakeRedis) mustSet(ctx context.Context, t *testing.T, key string, d *rpb.Digest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = make(map[string][]byte)
	}
	b, err := proto.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	f.m[key] = b
}

func (f *fakeRedis) Get(ctx context.Context, req *cachepb.GetReq, opts ...grpc.CallOption) (*cachepb.GetResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.m[req.Key]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "no digest cache for %s", req.Key)
	}
	return &cachepb.GetResp{
		Kv: &cachepb.KV{
			Key:   req.Key,
			Value: b,
		},
	}, nil
}

func (f *fakeRedis) Put(ctx context.Context, req *cachepb.PutReq, opts ...grpc.CallOption) (*cachepb.PutResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = make(map[string][]byte)
	}
	f.m[req.Kv.Key] = req.Kv.Value
	return &cachepb.PutResp{}, nil
}

// fakeCmdStorage represents fake cmdstorage bucket.
type fakeCmdStorage struct {
	m map[string]string // hash -> data
}

// Open opens cmd files identified by hash.
func (f *fakeCmdStorage) Open(ctx context.Context, hash string) (io.ReadCloser, error) {
	v, ok := f.m[hash]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s not found", hash)
	}
	return ioutil.NopCloser(strings.NewReader(v)), nil
}

// newFile stores a new file, and returns hash of it.
func (f *fakeCmdStorage) newFile(size int64) string {
	buf := make([]byte, size)
	rand.Read(buf)
	h := hash.SHA256Content(buf)
	if f.m == nil {
		f.m = make(map[string]string)
	}
	f.m[h] = string(buf)
	return h
}

// newFileSpec stores a new file and returns FileSpec of it.
func (f *fakeCmdStorage) newFileSpec(name string, isExecutable bool) *cpb.FileSpec {
	size := int64(rand.Uint64() % (8 * 1024 * 1024))
	h := f.newFile(size)
	return &cpb.FileSpec{
		Path:         name,
		Size:         size,
		Hash:         h,
		IsExecutable: isExecutable,
	}
}
