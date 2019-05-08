// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	sempb "github.com/bazelbuild/remote-apis/build/bazel/semver"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/uuid"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	opb "google.golang.org/genproto/googleapis/longrunning"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/remoteexec/cas"
	"go.chromium.org/goma/server/remoteexec/digest"
)

type operations struct {
	mu  sync.Mutex
	ops map[string][]*opb.Operation
}

func (o *operations) Add(opname string, op *opb.Operation) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ops == nil {
		o.ops = make(map[string][]*opb.Operation)
	}
	o.ops[opname] = append(o.ops[opname], op)
}

func (o *operations) Get(opname string) []*opb.Operation {
	o.mu.Lock()
	defer o.mu.Unlock()
	ops := o.ops[opname]
	o.ops[opname] = nil
	return ops
}

func execRespOp(opname string, resp *rpb.ExecuteResponse, err error) (*opb.Operation, error) {
	op := &opb.Operation{
		Name: opname,
		Done: true,
	}
	if err != nil {
		s, _ := status.FromError(err)
		op.Result = &opb.Operation_Error{
			Error: s.Proto(),
		}
		return op, nil
	}
	opresp := &opb.Operation_Response{}
	op.Result = opresp
	opresp.Response, err = ptypes.MarshalAny(resp)
	return op, err
}

type actionCache struct {
	mu sync.Mutex
	m  map[string]*rpb.ActionResult
}

func (c *actionCache) Get(actionDigest *rpb.Digest) (*rpb.ActionResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.m[cas.ResName("", actionDigest)]
	return r, ok
}

func (c *actionCache) Set(actionDigest *rpb.Digest, resp *rpb.ActionResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]*rpb.ActionResult)
	}
	c.m[cas.ResName("", actionDigest)] = resp
}

type fakeRBE struct {
	instancePrefix string

	fakeExec func(ctx context.Context, req *rpb.ExecuteRequest) (*rpb.ExecuteResponse, error)

	rpb.ExecutionServer
	ops operations

	rpb.ActionCacheServer
	cache actionCache

	rpb.ContentAddressableStorageServer
	bpb.ByteStreamServer
	cas *digest.Store

	rpb.CapabilitiesServer
	*rpb.ServerCapabilities

	execResp          *rpb.ExecuteResponse
	execErr           error
	gotExecuteRequest *rpb.ExecuteRequest
	gotAction         *rpb.Action
	gotCommand        *rpb.Command
}

func registerFakeRBE(srv *grpc.Server, rbe *fakeRBE) {
	rpb.RegisterExecutionServer(srv, rbe)
	rpb.RegisterActionCacheServer(srv, rbe)
	rpb.RegisterContentAddressableStorageServer(srv, rbe)
	bpb.RegisterByteStreamServer(srv, rbe)
	rpb.RegisterCapabilitiesServer(srv, rbe)
}

func defaultCapabilities() *rpb.ServerCapabilities {
	// as of Sep 14, capabilities is
	// cache_capabilities:<
	//  digest_function:SHA256
	//  action_cache_update_capabilities:<>
	//  max_batch_total_size_bytes:4194304
	//  symlink_absolute_path_strategy:DISALLOWED
	// >
	// execution_capabilities:<
	//  digest_function:SHA256
	//  exec_enabled:true
	// >
	// deprecated_api_version:<prerelease:"v1test" >
	// low_api_version:<major:2 >
	// high_api_version:<major:2 >
	return &rpb.ServerCapabilities{
		CacheCapabilities: &rpb.CacheCapabilities{
			DigestFunction: []rpb.DigestFunction{
				rpb.DigestFunction_SHA256,
			},
			ActionCacheUpdateCapabilities: &rpb.ActionCacheUpdateCapabilities{
				UpdateEnabled: false,
			},
			// CachePriorityCapabilities:
			MaxBatchTotalSizeBytes:      4 * 1024 * 1024,
			SymlinkAbsolutePathStrategy: rpb.CacheCapabilities_DISALLOWED,
		},
		ExecutionCapabilities: &rpb.ExecutionCapabilities{
			DigestFunction: rpb.DigestFunction_SHA256,
			ExecEnabled:    true,
			// ExecutionPriorityCapabilities:
		},
		LowApiVersion: &sempb.SemVer{
			Major: 2,
		},
		HighApiVersion: &sempb.SemVer{
			Major: 2,
		},
	}
}

const fakeInstancePrefix = "projects/goma-dev/instances"

func newFakeRBE() *fakeRBE {
	// TODO: validate capabilities.
	// - digest_function: sha256 only
	// - action cache update: not enabled
	// - symlink absolute: disallow
	// - exec enabled
	rbe := &fakeRBE{
		instancePrefix:     fakeInstancePrefix,
		cas:                digest.NewStore(),
		ServerCapabilities: defaultCapabilities(),

		execResp: &rpb.ExecuteResponse{
			Result: &rpb.ActionResult{
				ExitCode: 0,
			},
		},
	}
	rbe.fakeExec = func(ctx context.Context, req *rpb.ExecuteRequest) (*rpb.ExecuteResponse, error) {
		rbe.gotExecuteRequest = proto.Clone(req).(*rpb.ExecuteRequest)
		rbe.gotAction = &rpb.Action{}
		err := rbe.getProto(ctx, req.ActionDigest, rbe.gotAction)
		if err != nil {
			return nil, fmt.Errorf("no action in Execute Request %s: %v", req, err)
		}
		rbe.gotCommand = &rpb.Command{}
		err = rbe.getProto(ctx, rbe.gotAction.CommandDigest, rbe.gotCommand)
		if err != nil {
			return nil, fmt.Errorf("no command in Execute.Action %s: %v", rbe.gotAction, err)
		}
		return rbe.execResp, rbe.execErr
	}
	return rbe
}

func (f *fakeRBE) getProto(ctx context.Context, d *rpb.Digest, m proto.Message) error {
	if d == nil {
		return status.Errorf(codes.InvalidArgument, "no digest")
	}
	data, ok := f.cas.Get(d)
	if !ok {
		return status.Errorf(codes.NotFound, "no data for %s", d)
	}
	r, err := data.Open(ctx)
	if err != nil {
		return err
	}
	defer r.Close()
	buf, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	return proto.Unmarshal(buf, m)
}

func (f *fakeRBE) setProto(ctx context.Context, m proto.Message) (*rpb.Digest, error) {
	d, err := digest.Proto(m)
	if err != nil {
		return nil, err
	}
	f.cas.Set(d)
	return d.Digest(), nil
}

func (f *fakeRBE) isValidInstance(instance string) bool {
	return strings.HasPrefix(instance, f.instancePrefix+"/")
}

func (f *fakeRBE) verifyExecuteRequest(ctx context.Context, req *rpb.ExecuteRequest) error {
	if !f.isValidInstance(req.InstanceName) {
		return status.Errorf(codes.PermissionDenied, "unexpected instance name %q", req.InstanceName)
	}
	if req.ActionDigest == nil {
		return status.Errorf(codes.InvalidArgument, "no action digest")
	}
	action := &rpb.Action{}
	err := f.getProto(ctx, req.ActionDigest, action)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "action_digest: %v", err)
	}
	command := &rpb.Command{}
	err = f.getProto(ctx, action.CommandDigest, command)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "action.command_digest: %v", err)
	}
	if len(command.Arguments) == 0 {
		return status.Errorf(codes.InvalidArgument, "command.arguments empty")
	}
	if !sort.SliceIsSorted(command.EnvironmentVariables, func(i, j int) bool {
		return command.EnvironmentVariables[i].Name < command.EnvironmentVariables[j].Name
	}) {
		return status.Errorf(codes.InvalidArgument, "command.environment_variables: not sorted: %q", command.EnvironmentVariables)
	}

	if !sort.StringsAreSorted(command.OutputFiles) {
		return status.Errorf(codes.InvalidArgument, "command.output_files is not sorted: %q", command.OutputFiles)
	}
	if !sort.StringsAreSorted(command.OutputDirectories) {
		return status.Errorf(codes.InvalidArgument, "command.output_directories is not sorted: %q", command.OutputDirectories)
	}

	var prev string
	for _, fname := range append(append([]string{}, command.OutputFiles...), command.OutputDirectories...) {
		if strings.HasPrefix(fname, "/") || strings.HasSuffix(fname, "/") {
			return status.Errorf(codes.InvalidArgument, "command.output_* %q: leading slash or trailing slash", fname)
		}
		if fname == prev {
			return status.Errorf(codes.InvalidArgument, "command.output_* %q: duplicate", fname)
		}
		if prev != "" && strings.HasPrefix(fname, prev+"/") {
			return status.Errorf(codes.InvalidArgument, "command.output_* %q child of %q?", fname, prev)
		}
		prev = fname
	}

	if command.Platform == nil {
		return status.Errorf(codes.InvalidArgument, "command.platform: missing")
	}
	if !sort.SliceIsSorted(command.Platform.Properties, func(i, j int) bool {
		return command.Platform.Properties[i].Name < command.Platform.Properties[j].Name
	}) {
		return status.Errorf(codes.InvalidArgument, "command.platform.properties: not sorted")
	}

	err = f.verifyDirectory(ctx, action.InputRootDigest, ".")
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "action.input_root_digest: %v", err)
	}
	return nil
}

func (f *fakeRBE) verifyDirectory(ctx context.Context, d *rpb.Digest, name string) error {
	dir := &rpb.Directory{}
	err := f.getProto(ctx, d, dir)
	if err != nil {
		return err
	}
	if !sort.SliceIsSorted(dir.Files, func(i, j int) bool {
		return dir.Files[i].Name < dir.Files[j].Name
	}) {
		return fmt.Errorf("files not sorted at %s", name)
	}
	if !sort.SliceIsSorted(dir.Directories, func(i, j int) bool {
		return dir.Directories[i].Name < dir.Directories[j].Name
	}) {
		return fmt.Errorf("directories not sorted at %s", name)
	}
	if !sort.SliceIsSorted(dir.Symlinks, func(i, j int) bool {
		return dir.Symlinks[i].Name < dir.Symlinks[j].Name
	}) {
		return fmt.Errorf("symlinks not sorted at %s", name)
	}

	seen := make(map[string]interface{})
	for _, file := range dir.Files {
		if file.Name == "" || strings.Contains(file.Name, "/") {
			return fmt.Errorf("bad name %q for %s in %s", file.Name, file.Digest, name)
		}
		if seen[file.Name] != nil {
			return fmt.Errorf("duplicate %q in %s: %v", file.Name, name, dir)
		}
		seen[file.Name] = file
		_, ok := f.cas.Get(file.Digest)
		if !ok {
			return fmt.Errorf("%s %s not found", filepath.Join(name, file.Name), file.Digest)
		}
	}
	for _, d := range dir.Directories {
		if d.Name == "" || strings.Contains(d.Name, "/") {
			return fmt.Errorf("bad name %q for %s in %s", d.Name, d.Digest, name)
		}
		if seen[d.Name] != nil {
			return fmt.Errorf("duplicate %q in %s: %v", d.Name, name, dir)
		}
		seen[d.Name] = d
		err = f.verifyDirectory(ctx, d.Digest, filepath.Join(name, d.Name))
		if err != nil {
			return err
		}
	}
	for _, sym := range dir.Symlinks {
		if sym.Name == "" || strings.Contains(sym.Name, "/") {
			return fmt.Errorf("bad name %q to %s in %s", sym.Name, sym.Target, name)
		}
		if seen[sym.Name] != nil {
			return fmt.Errorf("duplicate %q in %s: %v", sym.Name, name, dir)
		}
		seen[sym.Name] = sym
		if strings.Contains(sym.Target, "/./") || strings.Contains(sym.Target, "//") {
			return fmt.Errorf("bad target %q: %s in %s", sym.Target, sym.Name, name)
		}
	}
	return nil
}

func (f *fakeRBE) Execute(req *rpb.ExecuteRequest, s rpb.Execution_ExecuteServer) error {
	ctx := s.Context()
	err := f.verifyExecuteRequest(ctx, req)
	if err != nil {
		return err
	}
	opname := uuid.New().String()
	if !req.SkipCacheLookup {
		aresp, err := f.GetActionResult(ctx, &rpb.GetActionResultRequest{
			InstanceName: req.InstanceName,
			ActionDigest: req.ActionDigest,
		})
		if err == nil {
			resp := &rpb.ExecuteResponse{
				Result:       proto.Clone(aresp).(*rpb.ActionResult),
				CachedResult: true,
				Status: &spb.Status{
					Code: int32(codes.OK),
				},
			}
			op, err := execRespOp(opname, resp, nil)
			if err != nil {
				return status.Errorf(codes.Internal, "execRespOp: %v", err)
			}
			return s.Send(op)
		}
	}
	var resp *rpb.ExecuteResponse
	if f.fakeExec == nil {
		err = status.Errorf(codes.Unavailable, "exec service unavailable")
	} else {
		resp, err = f.fakeExec(ctx, req)
	}
	op, err := execRespOp(opname, resp, err)
	if err != nil {
		return status.Errorf(codes.Internal, "execRespOp: %v", err)
	}
	f.ops.Add(opname, op)

	err = status.FromProto(resp.GetStatus()).Err()
	if err == nil && resp != nil && resp.Result != nil && resp.Result.ExitCode == 0 {
		f.cache.Set(req.ActionDigest, proto.Clone(resp.Result).(*rpb.ActionResult))
	}
	ops := f.ops.Get(opname)
	for _, op := range ops {
		err = s.Send(op)
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeRBE) WaitExecution(req *rpb.WaitExecutionRequest, s rpb.Execution_WaitExecutionServer) error {
	ctx := s.Context()
	logger := log.FromContext(ctx)
	name := req.Name
	logger.Infof("wait %q", name)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		ops := f.ops.Get(name)
		if len(ops) == 0 {
			return status.Errorf(codes.NotFound, "no operation for %q", name)
		}
		logger.Infof("operation for %q: %d", name, len(ops))
		for _, op := range ops {
			err := s.Send(op)
			if err != nil {
				return err
			}
		}
	}
}

func (f *fakeRBE) GetActionResult(ctx context.Context, req *rpb.GetActionResultRequest) (*rpb.ActionResult, error) {
	if !f.isValidInstance(req.InstanceName) {
		return nil, status.Errorf(codes.PermissionDenied, "unexpected instance name %q", req.InstanceName)
	}
	resp, ok := f.cache.Get(req.ActionDigest)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "no action cache")
	}
	return resp, nil
}

// TODO: UpdateActionResult

func (f *fakeRBE) FindMissingBlobs(ctx context.Context, req *rpb.FindMissingBlobsRequest) (*rpb.FindMissingBlobsResponse, error) {
	if !f.isValidInstance(req.InstanceName) {
		return nil, status.Errorf(codes.PermissionDenied, "unexpected instance name %q", req.InstanceName)
	}
	resp := &rpb.FindMissingBlobsResponse{}
	for _, d := range req.BlobDigests {
		_, ok := f.cas.Get(d)
		if !ok {
			resp.MissingBlobDigests = append(resp.MissingBlobDigests, d)
		}
	}
	return resp, nil
}

func (f *fakeRBE) BatchUpdateBlobs(ctx context.Context, req *rpb.BatchUpdateBlobsRequest) (*rpb.BatchUpdateBlobsResponse, error) {
	if !f.isValidInstance(req.InstanceName) {
		return nil, status.Errorf(codes.PermissionDenied, "unexpected instance name %q", req.InstanceName)
	}
	var totalSize int64
	resp := &rpb.BatchUpdateBlobsResponse{}
	for _, req := range req.Requests {
		d := req.Digest
		bresp := &rpb.BatchUpdateBlobsResponse_Response{
			Digest: d,
		}
		totalSize += int64(len(req.Data))
		resp.Responses = append(resp.Responses, bresp)
		v := digest.Bytes(cas.ResName("", d), req.Data)
		if !proto.Equal(d, v.Digest()) {
			bresp.Status = &spb.Status{
				Code:    int32(codes.InvalidArgument),
				Message: fmt.Sprintf("digest mismatch %s != %s", d, v.Digest()),
			}
			continue
		}
		f.cas.Set(v)
		bresp.Status = &spb.Status{
			Code: int32(codes.OK),
		}
	}
	if totalSize > f.ServerCapabilities.CacheCapabilities.MaxBatchTotalSizeBytes {
		return nil, status.Errorf(codes.InvalidArgument, "exceed server capabilities: %d > %d", totalSize, f.ServerCapabilities.CacheCapabilities.MaxBatchTotalSizeBytes)
	}
	return resp, nil
}

// TODO: GetTree?

func (f *fakeRBE) Read(req *bpb.ReadRequest, s bpb.ByteStream_ReadServer) error {
	if !f.isValidInstance(req.ResourceName) {
		return status.Errorf(codes.PermissionDenied, "unexpected instance name %q", req.ResourceName)
	}
	d, err := cas.ParseResName(strings.TrimPrefix(req.ResourceName, f.instancePrefix+"/"))
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "bad resource name %q: %v", req.ResourceName, err)
	}
	v, ok := f.cas.Get(d)
	if !ok {
		return status.Errorf(codes.NotFound, "%q not found", req.ResourceName)
	}
	ctx := s.Context()
	rd, err := v.Open(ctx)
	if err != nil {
		return status.Errorf(codes.Internal, "open %q: %v", req.ResourceName, err)
	}
	if req.ReadOffset < 0 {
		return status.Errorf(codes.OutOfRange, "read offset negative %d", req.ReadOffset)
	}
	if req.ReadOffset > 0 {
		_, err = io.CopyN(ioutil.Discard, rd, req.ReadOffset)
		if err != nil {
			return status.Errorf(codes.OutOfRange, "read offset %d: %v", req.ReadOffset, err)
		}
	}
	if req.ReadLimit < 0 {
		return status.Errorf(codes.OutOfRange, "read limit negative %d", req.ReadLimit)
	}
	if req.ReadLimit > 0 {
		rd = ioutil.NopCloser(&io.LimitedReader{
			R: rd,
			N: req.ReadLimit,
		})
	}
	const maxChunkSize = 2 * 1024 * 1024
	buf := make([]byte, maxChunkSize)
	for {
		n, err := rd.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		err = s.Send(&bpb.ReadResponse{
			Data: buf[:n],
		})
		if err != nil {
			return err
		}
	}
	// TODO: check digest hash?
	return nil
}

func (f *fakeRBE) Write(s bpb.ByteStream_WriteServer) error {
	var resourceName string
	var d *rpb.Digest
	var offset int64
	var buf bytes.Buffer
	for {
		req, err := s.Recv()
		if err != nil {
			return err
		}
		if req.ResourceName != "" {
			if resourceName == "" {
				resourceName = req.ResourceName
				if !f.isValidInstance(req.ResourceName) {
					return status.Errorf(codes.PermissionDenied, "unexpected instance name %q", req.ResourceName)
				}
				d, err = cas.ParseResName(strings.TrimPrefix(req.ResourceName, f.instancePrefix+"/"))
				if err != nil {
					return status.Errorf(codes.InvalidArgument, "bad resource name %q: %v", req.ResourceName, err)
				}
				// TODO: allow non zero offset?
			} else if req.ResourceName != resourceName {
				return status.Errorf(codes.InvalidArgument, "resource name mismatch %q => %q", resourceName, req.ResourceName)
			}
		}
		if resourceName == "" {
			return status.Errorf(codes.InvalidArgument, "no resource name")
		}
		if req.WriteOffset != offset {
			return status.Errorf(codes.InvalidArgument, "invalid offset %d; want=%d", req.WriteOffset, offset)
		}
		buf.Write(req.Data)
		offset += int64(len(req.Data))
		if req.FinishWrite {
			break
		}
	}
	data := digest.Bytes(cas.ResName("", d), buf.Bytes())
	if !proto.Equal(d, data.Digest()) {
		return status.Errorf(codes.InvalidArgument, "digest mismatch %s != %s", d, data.Digest())
	}
	f.cas.Set(data)
	return s.SendAndClose(&bpb.WriteResponse{
		CommittedSize: offset,
	})
}

// TODO: QueryWriteStatus?

func (f *fakeRBE) GetCapabilities(ctx context.Context, req *rpb.GetCapabilitiesRequest) (*rpb.ServerCapabilities, error) {
	if !f.isValidInstance(req.InstanceName) {
		return nil, status.Errorf(codes.PermissionDenied, "unexpected instance name %q", req.InstanceName)
	}
	return f.ServerCapabilities, nil
}
