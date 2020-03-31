/* Copyright 2018 Google Inc. All Rights Reserved. */

// Package cas manages content addressable storage.
package cas

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/golang/protobuf/proto"
	"go.opencensus.io/trace"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/remoteexec/datasource"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/rpc"
)

const (
	// DefaultBatchByteLimit is bytes limit for cas BatchUploadBlobs.
	DefaultBatchByteLimit = 4 * 1024 * 1024
	// BatchBlobLimit is max number of blobs in BatchUploadBlobs.
	batchBlobLimit = 1000
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

type batchUpdateBlobsRequestPool struct {
	mu        sync.Mutex
	pool      sync.Pool // New is protected by mu
	byteLimit int64
}

var batchReqPool batchUpdateBlobsRequestPool

// Get gets dummy BachUpdateBlobsRequest to check serialized size.
// maxSizeBytes is max size needed for data buffer.
// byteLimit is limit that RBE server sets for batch total size bytes.
// maxSizeBytes must not exceeds byteLimit.
func (p *batchUpdateBlobsRequestPool) Get(instance string, maxSizeBytes, byteLimit int64) *rpb.BatchUpdateBlobsRequest {
	p.mu.Lock()
	if p.byteLimit < byteLimit {
		p.byteLimit = byteLimit
	}
	if p.pool.New == nil {
		p.pool.New = func() interface{} {
			return make([]byte, 0, p.byteLimit)
		}
	}
	buf := p.pool.Get().([]byte)
	if int64(cap(buf)) < maxSizeBytes {
		buf = make([]byte, 0, maxSizeBytes)
	}
	p.mu.Unlock()
	return &rpb.BatchUpdateBlobsRequest{
		InstanceName: instance,
		Requests:     []*rpb.BatchUpdateBlobsRequest_Request{{Data: buf}},
	}
}

func (p *batchUpdateBlobsRequestPool) Put(req *rpb.BatchUpdateBlobsRequest) {
	p.pool.Put(req.Requests[0].Data)
}

func separateBlobsByByteLimit(blobs []*rpb.Digest, instance string, byteLimit int64) ([]*rpb.Digest, []*rpb.Digest) {
	if len(blobs) == 0 {
		return nil, nil
	}

	sort.Slice(blobs, func(i, j int) bool {
		return blobs[i].SizeBytes < blobs[j].SizeBytes
	})

	// Create dummy data to check protobuf size. To avoid redundant allocations, find the largest digest size.
	// string/bytes will be [encoded <wire-type><tag>] [length] [content...]
	// so no need to allocate more than byteLimit here.
	// https://developers.google.com/protocol-buffers/docs/encoding#structure
	// https://developers.google.com/protocol-buffers/docs/encoding#strings
	maxSizeBytes := blobs[len(blobs)-1].SizeBytes
	if maxSizeBytes >= byteLimit {
		maxSizeBytes = byteLimit
	}
	dummyReq := batchReqPool.Get(instance, maxSizeBytes, byteLimit)
	defer batchReqPool.Put(dummyReq)
	i := sort.Search(len(blobs), func(i int) bool {
		if blobs[i].SizeBytes >= byteLimit {
			return true
		}
		dummyReq.Requests[0].Digest = blobs[i]
		dummyReq.Requests[0].Data = dummyReq.Requests[0].Data[:blobs[i].SizeBytes]
		return int64(proto.Size(dummyReq)) >= byteLimit
	})
	if i < len(blobs) {
		return blobs[:i], blobs[i:]
	}
	return blobs, nil
}

func lookupBlobsInStore(ctx context.Context, blobs []*rpb.Digest, store *digest.Store, sema chan struct{}) ([]*rpb.BatchUpdateBlobsRequest_Request, []MissingBlob) {
	span := trace.FromContext(ctx)

	var wg sync.WaitGroup

	type blobLookupResult struct {
		err error
		req *rpb.BatchUpdateBlobsRequest_Request
	}
	results := make([]blobLookupResult, len(blobs))

	for i := range blobs {
		wg.Add(1)
		go func(blob *rpb.Digest, result *blobLookupResult) {
			defer wg.Done()
			sema <- struct{}{}
			defer func() {
				<-sema
			}()

			data, ok := store.Get(blob)
			if !ok {
				span.Annotatef(nil, "blob not found in cas: %v", blob)
				result.err = errBlobNotInReq
				return
			}
			b, err := datasource.ReadAll(ctx, data)
			if err != nil {
				span.Annotatef(nil, "blob data for %v: %v", blob, err)
				result.err = err
				return
			}
			// TODO: This is inefficient because we are reading all
			// sources whether or not they are going to be returned, due to the
			// size computation happening later. This might be okay as long as
			// we are not reading too much extra data in one operation.
			//
			// We should instead return all blob requests for blobs < `byteLimit`,
			// batched into multiple BatchUpdateBlobsRequests.
			result.req = &rpb.BatchUpdateBlobsRequest_Request{
				Digest: data.Digest(),
				Data:   b,
			}
		}(blobs[i], &results[i])
	}
	wg.Wait()

	var reqs []*rpb.BatchUpdateBlobsRequest_Request
	var missingBlobs []MissingBlob

	logger := log.FromContext(ctx)
	for i, result := range results {
		blob := blobs[i]
		if result.err != nil {
			missingBlobs = append(missingBlobs, MissingBlob{
				Digest: blob,
				Err:    result.err,
			})
			continue
		}
		if result.req != nil {
			reqs = append(reqs, result.req)
			continue
		}
		logger.Errorf("Lookup of blobs[%d]=%v yielded neither error nor request", i, blob)
	}
	return reqs, missingBlobs
}

func createBatchUpdateBlobsRequests(blobReqs []*rpb.BatchUpdateBlobsRequest_Request, instance string, byteLimit int64) []*rpb.BatchUpdateBlobsRequest {
	var batchReqs []*rpb.BatchUpdateBlobsRequest

	batchReqNoReqsSize := int64(proto.Size(&rpb.BatchUpdateBlobsRequest{InstanceName: instance}))
	size := batchReqNoReqsSize

	lastOffset := 0
	for i := range blobReqs {
		// This code assumes that all blobs in `blobReqs`, when added as the only element of
		// `batchReq.Requests`, will keep the marshaled proto size of `batchReq` < `byteLimit`.
		// If `byteLimit` is 0, then it is ignored.

		// Determine the extra proto size introduced by adding the current req.
		size += int64(proto.Size(&rpb.BatchUpdateBlobsRequest{Requests: blobReqs[i : i+1]}))

		// Add a new BatchUpdateBlobsRequest with blobs from the first blob after the
		// previous BatchUpdateBlobsRequest up to and including the current blob, if:
		// - this is the final blob
		// - adding this blob reaches the blob count limit
		// - adding the next blob pushes the size over the byte limit
		switch {
		case i == len(blobReqs)-1:
			fallthrough
		case i+1 == lastOffset+batchBlobLimit:
			fallthrough
		case byteLimit > 0 && size+int64(proto.Size(&rpb.BatchUpdateBlobsRequest{Requests: blobReqs[i+1 : i+2]})) > byteLimit:
			batchReqs = append(batchReqs, &rpb.BatchUpdateBlobsRequest{
				InstanceName: instance,
				Requests:     blobReqs[lastOffset : i+1],
			})
			size = batchReqNoReqsSize
			lastOffset = i + 1
		}
	}
	return batchReqs
}

// Upload uploads blobs stored in Store to instance of cas service.
func (c CAS) Upload(ctx context.Context, instance string, sema chan struct{}, blobs ...*rpb.Digest) error {
	span := trace.FromContext(ctx)
	logger := log.FromContext(ctx)
	logger.Infof("upload blobs %v", blobs)

	// up to max_batch_total_size_bytes, use BatchUpdateBlobs.
	// more than this, use bytestream.Write.
	byteLimit := int64(DefaultBatchByteLimit)
	if c.CacheCapabilities != nil && c.CacheCapabilities.MaxBatchTotalSizeBytes > 0 {
		byteLimit = c.CacheCapabilities.MaxBatchTotalSizeBytes
	}
	smallBlobs, largeBlobs := separateBlobsByByteLimit(blobs, instance, byteLimit)

	logger.Infof("upload by batch %d out of %d", len(smallBlobs), len(blobs))
	blobReqs, missingBlobs := lookupBlobsInStore(ctx, smallBlobs, c.Store, sema)
	missing := MissingError{
		Blobs: missingBlobs,
	}

	batchReqs := createBatchUpdateBlobsRequests(blobReqs, instance, byteLimit)
	for _, batchReq := range batchReqs {
		uploaded := false
		for !uploaded {
			t := time.Now()
			span.Annotatef(nil, "batch update %d blobs", len(batchReq.Requests))
			// TODO: should we report rpc error as missing input too?
			var batchResp *rpb.BatchUpdateBlobsResponse
			err := rpc.Retry{}.Do(ctx, func() error {
				var err error
				batchResp, err = c.Client.CAS().BatchUpdateBlobs(ctx, batchReq)
				return fixRBEInternalError(err)
			})
			if err != nil {
				if grpc.Code(err) == codes.ResourceExhausted {
					// gRPC returns ResourceExhausted if request message is larger than max.
					logger.Warnf("upload by batch %d blobs: %v", len(batchReq.Requests), err)
					// try with bytestream.
					// TODO: retry with fewer blobs?
					continue
				}

				return grpc.Errorf(grpc.Code(err), "batch update blobs: %v", err)
			}
			for _, res := range batchResp.Responses {
				st := status.FromProto(res.GetStatus())
				if st.Code() != codes.OK {
					span.Annotatef(nil, "batch update blob %v: %v", res.Digest, res.Status)
					return grpc.Errorf(st.Code(), "batch update blob %v: %v", res.Digest, res.Status)
				}
			}
			uploaded = true
			logger.Infof("upload by batch %d blobs in %s", len(batchReq.Requests), time.Since(t))
		}
	}
	logger.Infof("upload by streaming from %d out of %d", len(largeBlobs), len(blobs))
	for _, blob := range largeBlobs {
		data, ok := c.Store.Get(blob)
		if !ok {
			span.Annotatef(nil, "blob not found in cas: %v", blob)
			missing.Blobs = append(missing.Blobs, MissingBlob{
				Digest: blob,
				Err:    errBlobNotInReq,
			})
			continue
		}
		err := rpc.Retry{}.Do(ctx, func() error {
			rd, err := data.Open(ctx)
			if err != nil {
				span.Annotatef(nil, "upload open %v: %v", blob, err)
				missing.Blobs = append(missing.Blobs, MissingBlob{
					Digest: blob,
					Err:    err,
				})
				return err
			}
			err = UploadDigest(ctx, c.Client.ByteStream(), instance, blob, rd)
			if err != nil {
				rd.Close()
				return fixRBEInternalError(err)
			}
			rd.Close()
			return nil
		})
		if err != nil {
			logger.Errorf("upload streaming %s error: %v", blob, err)
			continue
		}
	}
	if len(missing.Blobs) > 0 {
		return missing
	}
	return nil
}

func fixRBEInternalError(err error) error {
	if status.Code(err) == codes.Internal {
		return status.Errorf(codes.Unavailable, "%v", err)
	}
	return err
}
