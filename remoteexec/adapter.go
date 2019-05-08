// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package remoteexec provides proxy to remoteexec server.
//
// TODO: goma client should use credential described in
// https://cloud.google.com/remote-build-execution/docs/getting-started
package remoteexec

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"go.chromium.org/goma/server/server"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/metadata"

	"go.chromium.org/goma/server/auth/enduser"
	"go.chromium.org/goma/server/exec"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	fpb "go.chromium.org/goma/server/proto/file"
	"go.chromium.org/goma/server/remoteexec/cas"
	"go.chromium.org/goma/server/remoteexec/digest"
)

// DigetCache caches digest for goma file hash.
type DigestCache interface {
	Get(context.Context, string, digest.Source) (digest.Data, error)
}

// Adapter is an adapter from goma API to remoteexec API.
type Adapter struct {
	// InstancePrefix is the prefix (dirname) of the full RBE instance name.
	// e.g. If instance name == "projects/$PROJECT/instances/default_instance",
	// then InstancePrefix is "projects/$PROJECT/instances"
	InstancePrefix string

	Inventory   exec.Inventory
	ExecTimeout time.Duration

	// Client is remoteexec API client.
	Client Client

	// GomaFile handles output files from remoteexec's cas to goma's FileBlob.
	GomaFile fpb.FileServiceClient

	// key: goma file hash.
	DigestCache DigestCache

	// CmdStorage is a storage for command files.
	CmdStorage CmdStorage

	// Tool details put in request metadata.
	ToolDetails *rpb.ToolDetails

	// FileLookupConcurrency represents concurrency to look up file
	// contents from file-cache-server to be converted to CAS.
	// 1 if not specified.
	FileLookupConcurrency int

	capMu        sync.Mutex
	capabilities *rpb.ServerCapabilities
}

func (f *Adapter) withRequestMetadata(ctx context.Context, reqInfo *gomapb.RequesterInfo) (context.Context, error) {
	rmd := &rpb.RequestMetadata{
		ToolDetails:      f.ToolDetails,
		ActionId:         reqInfo.GetCompilerProxyId(),
		ToolInvocationId: reqInfo.GetBuildId(),
		// TODO: b/77176746
		// CorrelatedInvocationsId:
	}
	// TODO: remove the following workaround.
	//                    when autoninja is used for the build,
	//                    and client is rolled, we do not need
	//                    the following workaround.
	//
	// workaround b/77176764
	// set compiler_proxy_id prefix to ToolInvocationId if chrome-bot@
	// chrome-bot@ (aka buildbot) start/stop compiler_proxy per each
	// compile step, so compiler_proxy_id prefix would be good fit
	// for ToolInvocationId.
	// Typical user keeps running compiler_proxy, so same
	// compiler_proxy_id prefix will be used for several ninja invocation.
	// We cannot use that for human users.
	id := reqInfo.GetCompilerProxyId()
	if rmd.ToolInvocationId == "" && strings.HasPrefix(id, "chrome-bot@") {
		i := strings.LastIndexByte(id, '/')
		if i > 0 {
			rmd.ToolInvocationId = id[:i]
		}
	}

	logger := log.FromContext(ctx)
	logger.Infof("request metadata: %s", rmd)
	b, err := proto.Marshal(rmd)
	if err != nil {
		return nil, err
	}
	// https://github.com/grpc/grpc-go/blob/master/Documentation/grpc-metadata.md#storing-binary-data-in-metadata
	return metadata.AppendToOutgoingContext(ctx,
		"google.devtools.remoteexecution.v1test.requestmetadata-bin",
		string(b)), nil
}

type contextKey int

var requesterInfoKey contextKey

func (f *Adapter) outgoingContext(ctx context.Context, reqInfo *gomapb.RequesterInfo) context.Context {
	logger := log.FromContext(ctx)
	ctx, err := f.withRequestMetadata(ctx, reqInfo)
	if err != nil {
		logger.Errorf("request metadata(%v)=_, %v", reqInfo, err)
		return ctx
	}
	if reqInfo != nil {
		reqInfo = proto.Clone(reqInfo).(*gomapb.RequesterInfo)
	} else {
		reqInfo = &gomapb.RequesterInfo{}
	}
	reqInfo.Addr = proto.String(server.HostName(ctx))
	return context.WithValue(ctx, requesterInfoKey, reqInfo)
}

func requesterInfo(ctx context.Context) *gomapb.RequesterInfo {
	r, _ := ctx.Value(requesterInfoKey).(*gomapb.RequesterInfo)
	return r
}

func (f *Adapter) client(ctx context.Context) Client {
	user, _ := enduser.FromContext(ctx)
	client := f.Client
	token := user.Token()
	if token.AccessToken == "" {
		return client
	}
	client.CallOptions = append(client.CallOptions,
		grpc.PerRPCCredentials(oauth.NewOauthAccess(token)))
	return client
}

func (f *Adapter) DefaultInstance() string {
	return path.Join(f.InstancePrefix, "default_instance")
}

func (f *Adapter) ensureCapabilities(ctx context.Context) {
	f.capMu.Lock()
	defer f.capMu.Unlock()

	if f.capabilities != nil {
		return
	}
	logger := log.FromContext(ctx)
	var err error
	f.capabilities, err = f.client(ctx).GetCapabilities(ctx, &rpb.GetCapabilitiesRequest{
		InstanceName: f.DefaultInstance(),
	})
	if err != nil {
		logger.Errorf("GetCapabilities: %v", err)
		return
	}
	logger.Infof("serverCapabilities: %v", f.capabilities)
}

func (f *Adapter) newRequest(ctx context.Context, gomaReq *gomapb.ExecReq) *request {
	logger := log.FromContext(ctx)
	gs := digest.NewStore()
	timeout := f.ExecTimeout
	if timeout == 0 {
		timeout = 600 * time.Second
	}
	client := f.client(ctx)
	r := &request{
		f:      f,
		client: client,
		cas: &cas.CAS{
			Client:            client,
			Store:             gs,
			CacheCapabilities: f.capabilities.GetCacheCapabilities(),
		},
		gomaReq: gomaReq,
		gomaResp: &gomapb.ExecResp{
			Result: &gomapb.ExecResult{
				ExitStatus: proto.Int32(-1),
			},
		},
		digestStore: gs,
		action: &rpb.Action{
			Timeout:    ptypes.DurationProto(timeout),
			DoNotCache: doNotCache(gomaReq),
		},
	}
	logger.Infof("%s: new request", r.ID())
	return r
}

// Exec handles goma Exec requests with remoteexec backend.
//
//  1. compute input tree and Action.
//  1.1 construct input tree from req.
//  1.2. construct Action message from req.
//  2. checks the ActionCache using GetActionResult. if hit, go to 7.
//  3. queries the ContentAddressableStorage using FindMissingBlobs
//  4. uploads any missing blobs to the ContentAddressableStorage
//     using bytestream.Write and BatchUpdateBlobs.
//  5. executes the action using Execute.
//  6. awaits completion of the action using the longrunning.Operations.
//  7. looks a the ActionResult
//  8. If the action is successful, uses bytestream.Read to download any outputs
//     it does not already have;
//     embed it in response, or will serve it by LookupFile later
//  9. job is complete
//  9.1  convert ExecResp from ExecuteResponse.
//       for small outputs, embed in resp. otherwise use FILE_META.
func (f *Adapter) Exec(ctx context.Context, req *gomapb.ExecReq) (resp *gomapb.ExecResp, err error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/remoteexec.Adapter.Exec")
	defer span.End()

	logger := log.FromContext(ctx)
	defer func() {
		if err != nil {
			return
		}
		err := exec.RecordAPIError(ctx, resp)
		if err != nil {
			logger.Errorf("failed to record stats: %v", err)
		}
	}()

	t0 := time.Now()
	adjustExecReq(req)
	ctx = f.outgoingContext(ctx, req.GetRequesterInfo())
	f.ensureCapabilities(ctx)

	r := f.newRequest(ctx, req)
	t := time.Now()

	resp = r.getInventoryData(ctx)
	if resp != nil {
		logger.Infof("fail fast in inventory lookup: %s", time.Since(t))
		return resp, nil
	}

	resp = r.newInputTree(ctx)
	if resp != nil {
		logger.Infof("fail fast in input tree: %s", time.Since(t))
		return resp, nil
	}
	r.setupNewAction(ctx)

	// Use this to collect all timestamps and then print on one line, regardless of where
	// this function returns.
	var timestamps []string
	defer func() {
		logger.Infof("%s", strings.Join(timestamps, ", "))
	}()
	addTimestamp := func(desc string, duration time.Duration) {
		timestamps = append(timestamps, fmt.Sprintf("%s: %s", desc, duration))
	}

	addTimestamp("setup", time.Since(t))

	eresp := &rpb.ExecuteResponse{}
	var cached bool
	eresp.Result, cached = r.checkCache(ctx)
	if !cached {
		t = time.Now()
		blobs := r.missingBlobs(ctx)
		addTimestamp("check missing", time.Since(t))
		t = time.Now()
		resp := r.uploadBlobs(ctx, blobs)
		if resp != nil {
			addTimestamp("fail fast for missing input", time.Since(t))
			return resp, nil
		}
		addTimestamp("upload blobs", time.Since(t))

		t = time.Now()
		var err error
		eresp, err = r.executeAction(ctx)
		addTimestamp("execute", time.Since(t))
		if err != nil {
			logger.Infof("execute err=%v", err)
			return nil, err
		}
	}
	t = time.Now()
	resp, err = r.newResp(ctx, eresp, cached)
	addTimestamp("response", time.Since(t))
	addTimestamp("total", time.Since(t0))
	return resp, err
}
