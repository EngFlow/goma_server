// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"context"
	"time"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	lpb "google.golang.org/genproto/googleapis/longrunning"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/rpc"
)

var (
	numRunningOperations = stats.Int64(
		"go.chromium.org/goma/server/remoteexec.running-operations",
		"Number of current running exec operations",
		stats.UnitDimensionless)

	DefaultViews = []*view.View{
		{
			Description: `Number of current running exec operations`,
			Measure:     numRunningOperations,
			Aggregation: view.Sum(),
		},
	}
)

// Client is a remoteexec API client to ClientConn.
// CallOptions will be added when calling RPC.
//
//  prcred, _ := oauth.NewApplicationDefault(ctx,
//     "https://www.googleapis.com/auth/cloud-build-service")
//  conn, _ := grpc.DialContext(ctx, target,
//    grpc.WithPerRPCCredentials(prcred),
//    grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})))
//  client := &remoteexec.Client{conn}
type Client struct {
	*grpc.ClientConn
	CallOptions []grpc.CallOption
}

func (c Client) callOptions(opts ...grpc.CallOption) []grpc.CallOption {
	return append(append([]grpc.CallOption(nil), opts...), c.CallOptions...)
}

// Cache returns action cache client.
// https://github.com/bazelbuild/remote-apis/blob/c1c1ad2c97ed18943adb55f06657440daa60d833/build/bazel/remote/execution/v2/remote_execution.proto#L117
func (c Client) Cache() rpb.ActionCacheClient {
	return c
}

// GetActionResult retrieves a cached execution result.
func (c Client) GetActionResult(ctx context.Context, req *rpb.GetActionResultRequest, opts ...grpc.CallOption) (*rpb.ActionResult, error) {
	return rpb.NewActionCacheClient(c.ClientConn).GetActionResult(ctx, req, c.callOptions(opts...)...)
}

// UpdateActionResult uploads a new execution result.
func (c Client) UpdateActionResult(ctx context.Context, req *rpb.UpdateActionResultRequest, opts ...grpc.CallOption) (*rpb.ActionResult, error) {
	return rpb.NewActionCacheClient(c.ClientConn).UpdateActionResult(ctx, req, c.callOptions(opts...)...)
}

// Exec returns execution client.
// https://github.com/bazelbuild/remote-apis/blob/c1c1ad2c97ed18943adb55f06657440daa60d833/build/bazel/remote/execution/v2/remote_execution.proto#L34
func (c Client) Exec() rpb.ExecutionClient {
	return c
}

// Execute executes an action remotely.
func (c Client) Execute(ctx context.Context, req *rpb.ExecuteRequest, opts ...grpc.CallOption) (rpb.Execution_ExecuteClient, error) {
	return rpb.NewExecutionClient(c.ClientConn).Execute(ctx, req, c.callOptions(opts...)...)
}

// WaitExecution waits for an execution operation to complete.
func (c Client) WaitExecution(ctx context.Context, req *rpb.WaitExecutionRequest, opts ...grpc.CallOption) (rpb.Execution_WaitExecutionClient, error) {
	return rpb.NewExecutionClient(c.ClientConn).WaitExecution(ctx, req, c.callOptions(opts...)...)
}

// CAS returns content addressable storage client.
// https://github.com/bazelbuild/remote-apis/blob/c1c1ad2c97ed18943adb55f06657440daa60d833/build/bazel/remote/execution/v2/remote_execution.proto#L168
func (c Client) CAS() rpb.ContentAddressableStorageClient {
	return c
}

// FindMissingBlobs determines if blobs are present in the CAS.
func (c Client) FindMissingBlobs(ctx context.Context, req *rpb.FindMissingBlobsRequest, opts ...grpc.CallOption) (*rpb.FindMissingBlobsResponse, error) {
	return rpb.NewContentAddressableStorageClient(c.ClientConn).FindMissingBlobs(ctx, req, c.callOptions(opts...)...)
}

// BatchUpdateBlobs uploads many blobs at once.
func (c Client) BatchUpdateBlobs(ctx context.Context, req *rpb.BatchUpdateBlobsRequest, opts ...grpc.CallOption) (*rpb.BatchUpdateBlobsResponse, error) {
	return rpb.NewContentAddressableStorageClient(c.ClientConn).BatchUpdateBlobs(ctx, req, c.callOptions(opts...)...)
}

// BatchReadBlobs downloads many blobs at once.
func (c Client) BatchReadBlobs(ctx context.Context, req *rpb.BatchReadBlobsRequest, opts ...grpc.CallOption) (*rpb.BatchReadBlobsResponse, error) {
	return rpb.NewContentAddressableStorageClient(c.ClientConn).BatchReadBlobs(ctx, req, c.callOptions(opts...)...)
}

// GetTree fetches the entire directory tree rooted at a node.
func (c Client) GetTree(ctx context.Context, req *rpb.GetTreeRequest, opts ...grpc.CallOption) (rpb.ContentAddressableStorage_GetTreeClient, error) {
	return rpb.NewContentAddressableStorageClient(c.ClientConn).GetTree(ctx, req, c.callOptions(opts...)...)
}

// ByteStream returns byte stream client.
// https://godoc.org/google.golang.org/genproto/googleapis/bytestream#ByteStreamClient
func (c Client) ByteStream() bpb.ByteStreamClient {
	return c
}

// Read is used to retrieve the contents of a resource as a sequence of bytes.
func (c Client) Read(ctx context.Context, in *bpb.ReadRequest, opts ...grpc.CallOption) (bpb.ByteStream_ReadClient, error) {
	return bpb.NewByteStreamClient(c.ClientConn).Read(ctx, in, c.callOptions(opts...)...)
}

// Write is used to send the contents of a resource as a sequence of bytes.
func (c Client) Write(ctx context.Context, opts ...grpc.CallOption) (bpb.ByteStream_WriteClient, error) {
	return bpb.NewByteStreamClient(c.ClientConn).Write(ctx, c.callOptions(opts...)...)
}

// QueryWriteStatus is used to find the committed_size for a resource
// that is being written, which can be then be used as the write_offset
// for the next Write call.
func (c Client) QueryWriteStatus(ctx context.Context, in *bpb.QueryWriteStatusRequest, opts ...grpc.CallOption) (*bpb.QueryWriteStatusResponse, error) {
	return bpb.NewByteStreamClient(c.ClientConn).QueryWriteStatus(ctx, in, c.callOptions(opts...)...)
}

// Capabilities returns capabilities client.
func (c Client) Capabilities() rpb.CapabilitiesClient {
	return c
}

// GetCapabilities returns the server capabilities configuration.
func (c Client) GetCapabilities(ctx context.Context, req *rpb.GetCapabilitiesRequest, opts ...grpc.CallOption) (*rpb.ServerCapabilities, error) {
	return rpb.NewCapabilitiesClient(c.ClientConn).GetCapabilities(ctx, req, c.callOptions(opts...)...)
}

func logOpMetadata(logger log.Logger, op *lpb.Operation) {
	if op.GetMetadata() == nil {
		logger.Infof("operation update: no metadata")
		return
	}
	md := &rpb.ExecuteOperationMetadata{}
	err := ptypes.UnmarshalAny(op.GetMetadata(), md)
	if err != nil {
		logger.Warnf("operation update: %s: metadata bad type %T: %v", op.GetName(), op.GetMetadata(), err)
		return
	}
	logger.Infof("operation update: %s: %v", op.GetName(), md)
}

// ExecuteAndWait executes and action remotely and wait its response.
// it returns operation name, response and error.
func ExecuteAndWait(ctx context.Context, c Client, req *rpb.ExecuteRequest, opts ...grpc.CallOption) (string, *rpb.ExecuteResponse, error) {
	logger := log.FromContext(ctx)
	logger.Infof("execute action")

	recordStart := func() {
		stats.Record(ctx, numRunningOperations.M(1))
	}
	recordFinish := func() {
		stats.Record(ctx, numRunningOperations.M(-1))
	}

	var opName string
	var waitReq *rpb.WaitExecutionRequest
	resp := &rpb.ExecuteResponse{}
	type responseStream interface {
		Recv() (*lpb.Operation, error)
	}
	err := rpc.Retry{}.Do(ctx, func() error {
		ctx, cancel := context.WithTimeout(ctx, 1*time.Minute)
		defer cancel()
		var stream responseStream
		var err error
		if waitReq != nil {
			stream, err = c.Exec().WaitExecution(ctx, waitReq, opts...)
		} else {
			recordStart()
			stream, err = c.Exec().Execute(ctx, req, opts...)
		}
		if err != nil {
			return grpc.Errorf(grpc.Code(err), "execute: %v", err)
		}
		for {
			op, err := stream.Recv()
			if err != nil {
				// if not found, retry from execute
				// otherwise, rerun from WaitExecution.
				if status.Code(err) == codes.NotFound {
					waitReq = nil
					recordFinish()
					return status.Errorf(codes.Unavailable, "operation stream lost: %v", err)
				}
				return err
			}
			if opName == "" {
				opName = op.GetName()
				logger.Infof("operation starts: %s", opName)
			}
			if !op.GetDone() {
				logOpMetadata(logger, op)
				waitReq = &rpb.WaitExecutionRequest{
					Name: opName,
				}
				continue
			}
			waitReq = nil
			err = ptypes.UnmarshalAny(op.GetResponse(), resp)
			if err != nil {
				err = status.Errorf(codes.Internal, "op %s response bad type %T: %v", op.GetName(), op.GetResponse(), err)
				logger.Errorf("%s", err)
				return err
			}
			return erespErr(resp)
		}
	})
	recordFinish()
	return opName, resp, err
}

// erespErr returns codes.Unavailable if it has retriable failure result.
// returns nil otherwise (to terminates retrying, even if eresp contains
// error status).
func erespErr(eresp *rpb.ExecuteResponse) error {
	st := eresp.GetStatus()
	// see bazel remote retrier.
	// https://github.com/bazelbuild/bazel/blob/master/src/main/java/com/google/devtools/build/lib/remote/RemoteRetrier.java
	// also see https://cloud.google.com/pubsub/docs/reference/error-codes
	//
	// we don't retry codes.Unauthenticated; end user's access token would
	// be expired, so retry also get unauthenticated (we don't have
	// refresh token).
	//
	// codes.DeadlineExceeded might fail soon with codes.DeadlineExceeded,
	// but if server side sets shorter deadline and caller has more time
	// in context, retry would succeed.
	switch codes.Code(st.GetCode()) {
	case codes.Unknown, codes.DeadlineExceeded, codes.Aborted, codes.Internal, codes.Unavailable, codes.ResourceExhausted:
		st = proto.Clone(st).(*spb.Status)
		// codes.Unavailable, so that rpc.Retry will retry.
		st.Code = int32(codes.Unavailable)
		return status.FromProto(st).Err()
	}
	return nil
}
