/* Copyright 2018 Google Inc. All Rights Reserved. */

// Package cas manages content addressable storage.
package cas

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.opencensus.io/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	bpb "google.golang.org/genproto/googleapis/bytestream"

	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/remoteexec/datasource"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/rpc"
)

// Client is a client of cas service.
type Client interface {
	CAS() rpb.ContentAddressableStorageClient
	ByteStream() bpb.ByteStreamClient
}

type client struct {
	*grpc.ClientConn
}

func (c client) CAS() rpb.ContentAddressableStorageClient {
	return rpb.NewContentAddressableStorageClient(c.ClientConn)
}

func (c client) ByteStream() bpb.ByteStreamClient {
	return bpb.NewByteStreamClient(c.ClientConn)
}

// NewClient creates client from conn.
func NewClient(conn *grpc.ClientConn) Client {
	return client{conn}
}

// CAS is content-addressable-storage synced between local and cas service.
type CAS struct {
	Client
	*digest.Store

	CacheCapabilities *rpb.CacheCapabilities
}

// TODO: unit test

// Missing checks blobs in local exists in instance of cas service,
// and returns missing blobs.
func (c CAS) Missing(ctx context.Context, instance string, blobs []*rpb.Digest) ([]*rpb.Digest, error) {
	span := trace.FromContext(ctx)
	logger := log.FromContext(ctx)
	logger.Infof("check %d blobs in %s", len(blobs), instance)
	span.Annotatef(nil, "check %d blobs", len(blobs))
	resp, err := c.Client.CAS().FindMissingBlobs(ctx, &rpb.FindMissingBlobsRequest{
		InstanceName: instance,
		BlobDigests:  blobs,
	})
	if err != nil {
		return nil, grpc.Errorf(grpc.Code(err), "missing blobs: %v", err)
	}
	span.Annotatef(nil, "missings %d blobs", len(resp.MissingBlobDigests))
	logger.Infof("missings %v", resp.MissingBlobDigests)
	return resp.MissingBlobDigests, nil
}

var (
	errBlobNotInReq = errors.New("blob not in request")
)

// MissingBlob is a missing blog.
type MissingBlob struct {
	Digest *rpb.Digest
	Err    error
}

// MissingError is an error about missing content for blobs.
type MissingError struct {
	Blobs []MissingBlob
}

func (e MissingError) Error() string {
	return fmt.Sprintf("missing %d blobs", len(e.Blobs))
}

// Upload uploads blobs stored in Store to instance of cas service.
func (c CAS) Upload(ctx context.Context, instance string, blobs ...*rpb.Digest) error {
	span := trace.FromContext(ctx)
	logger := log.FromContext(ctx)
	logger.Infof("upload blobs %v", blobs)
	// sort by size_bytes.
	sort.Slice(blobs, func(i, j int) bool {
		return blobs[i].SizeBytes < blobs[j].SizeBytes
	})
	var missing MissingError

	// up to max_batch_total_size_bytes, use BatchUpdateBlobs.
	// more than this, use bytestream.Write.

	// TODO: better packing
	var i int

	const defaultBatchLimit = 4 * 1024 * 1024
	batchLimit := int64(defaultBatchLimit)
	if c.CacheCapabilities != nil && c.CacheCapabilities.MaxBatchTotalSizeBytes > 0 {
		batchLimit = c.CacheCapabilities.MaxBatchTotalSizeBytes
	}
Loop:
	for i < len(blobs) {
		if blobs[i].SizeBytes >= batchLimit {
			break
		}
		batchReq := &rpb.BatchUpdateBlobsRequest{
			InstanceName: instance,
		}
		var size int64
		i0 := i
		for ; i < len(blobs); i++ {
			size += blobs[i].SizeBytes
			if size >= batchLimit {
				break
			}
			data, ok := c.Store.Get(blobs[i])
			if !ok {
				span.Annotatef(nil, "blob not found in cas: %v", blobs[i])
				missing.Blobs = append(missing.Blobs, MissingBlob{
					Digest: blobs[i],
					Err:    errBlobNotInReq,
				})
				continue
			}
			b, err := datasource.ReadAll(ctx, data)
			if err != nil {
				span.Annotatef(nil, "blob data for %v: %v", blobs[i], err)
				missing.Blobs = append(missing.Blobs, MissingBlob{
					Digest: blobs[i],
					Err:    err,
				})
				continue
			}
			batchReq.Requests = append(batchReq.Requests, &rpb.BatchUpdateBlobsRequest_Request{
				Digest: data.Digest(),
				Data:   b,
			})
		}
		logger.Infof("upload by batch [%d,%d) out of %d", i0, i, len(blobs))
		t := time.Now()
		span.Annotatef(nil, "batch update %d blobs", len(batchReq.Requests))
		// TODO: should we report rpc error as missing input too?
		var batchResp *rpb.BatchUpdateBlobsResponse
		err := rpc.Retry{}.Do(ctx, func() error {
			var err error
			batchResp, err = c.Client.CAS().BatchUpdateBlobs(ctx, batchReq)
			return err
		})
		if err != nil {
			if grpc.Code(err) == codes.ResourceExhausted {
				// gRPC returns ResourceExhausted if request message is larger than max.
				logger.Warnf("upload by batch [%d,%d): %v", i0, i, err)
				// try with bytestream.
				// TODO: retry with fewer blobs?
				i = i0
				break Loop
			}

			return grpc.Errorf(grpc.Code(err), "batch update blobs: %v", err)
		}
		for _, res := range batchResp.Responses {
			if codes.Code(res.Status.Code) != codes.OK {
				span.Annotatef(nil, "batch update blob %v: %v", res.Digest, res.Status)
				return grpc.Errorf(codes.Code(res.Status.Code), "batch update blob %v: %v", res.Digest, res.Status)
			}
		}
		logger.Infof("upload by batch %d blobs in %s", len(batchReq.Requests), time.Since(t))
	}
	logger.Infof("upload by streaming from %d out of %d", i, len(blobs))
	for ; i < len(blobs); i++ {
		data, ok := c.Store.Get(blobs[i])
		if !ok {
			span.Annotatef(nil, "blob not found in cas: %v", blobs[i])
			missing.Blobs = append(missing.Blobs, MissingBlob{
				Digest: blobs[i],
				Err:    errBlobNotInReq,
			})
			continue
		}
		rd, err := data.Open(ctx)
		if err != nil {
			span.Annotatef(nil, "upload open %v: %v", blobs[i], err)
			missing.Blobs = append(missing.Blobs, MissingBlob{
				Digest: blobs[i],
				Err:    err,
			})
			continue
		}
		err = UploadDigest(ctx, c.Client.ByteStream(), instance, blobs[i], rd)
		if err != nil {
			rd.Close()
			return err
		}
		rd.Close()
	}
	if len(missing.Blobs) > 0 {
		return missing
	}
	return nil
}
