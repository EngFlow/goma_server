// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package cas

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"

	rdigest "github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"go.chromium.org/goma/server/remoteexec/digest"
)

type blobData struct {
	digest *rpb.Digest
	data   []byte
}

func makeBlobData(data string) *blobData {
	hash := sha256.Sum256([]byte(data))
	return &blobData{
		digest: &rpb.Digest{
			Hash:      fmt.Sprintf("%x", hash),
			SizeBytes: int64(len(data)),
		},
		data: []byte(data),
	}
}

func getDigests(bds []*blobData) []*rpb.Digest {
	var result []*rpb.Digest
	for _, bd := range bds {
		result = append(result, bd.digest)
	}
	return result
}

func concatDigests(digests ...[]*rpb.Digest) []*rpb.Digest {
	var result []*rpb.Digest
	for _, digest := range digests {
		result = append(result, digest...)
	}
	return result
}

func blobDataToBatchUpdateReq(b *blobData) *rpb.BatchUpdateBlobsRequest_Request {
	return &rpb.BatchUpdateBlobsRequest_Request{
		Digest: b.digest,
		Data:   b.data,
	}
}

func protoEqual(x, y interface{}) bool {
	return cmp.Equal(x, y, cmp.Comparer(proto.Equal), cmpopts.EquateEmpty())
}

func TestMissing(t *testing.T) {
	// Blobs already in CAS.
	presentBlobs := []*blobData{
		makeBlobData("5WGm1JJ1x77KSrlRgzxL"),
		makeBlobData("ZJ0BiCaayupcdD2nRTmXXrre772lCF"),
		makeBlobData("o2JzZO7qr6dwwR2CmXZtWDJ65ZkT885aruPAe0nm"),
	}
	// Blobs not in CAS.
	missingBlobs := []*rpb.Digest{
		{
			Hash:      "1a77aacc1ed3ea410230d66f1238d5a8",
			SizeBytes: 50,
		},
		{
			Hash:      "bad2614f186bf481ee339896089825b5",
			SizeBytes: 60,
		},
		{
			Hash:      "6f2bf26893e588575985446bf9fd116e",
			SizeBytes: 70,
		},
	}

	allBlobs := append(getDigests(presentBlobs), missingBlobs...)

	for _, tc := range []struct {
		desc         string
		blobs        []*rpb.Digest
		presentBlobs []*blobData
		wantMissing  []*rpb.Digest
	}{
		{
			desc:        "empty CAS",
			blobs:       allBlobs,
			wantMissing: allBlobs,
		},
		{
			desc:         "only present blobs",
			blobs:        allBlobs[:3],
			presentBlobs: presentBlobs,
		},
		{
			desc:         "present and missing blobs",
			blobs:        allBlobs,
			presentBlobs: presentBlobs,
			wantMissing:  missingBlobs,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			instance := "instance"
			fc, err := newFakeCASClient(0, instance)
			defer fc.teardown()
			if err != nil {
				t.Errorf("err=%q, want nil", err)
				return
			}
			for _, blob := range tc.presentBlobs {
				fc.server.cas.Put(blob.data)
			}

			cas := CAS{Client: fc}
			ctx := context.Background()
			missing, err := cas.Missing(ctx, instance, tc.blobs)
			if err != nil {
				t.Errorf("err=%q; want nil", err)
			}
			if !protoEqual(missing, tc.wantMissing) {
				t.Errorf("missing=%q; want=%q", missing, tc.wantMissing)
			}
		})
	}
}

func TestSeparateBlobsByByteLimit(t *testing.T) {
	blobs := []*rpb.Digest{
		{
			Hash:      "5baa6de0968b9ef4607ea7c62f847c4b",
			SizeBytes: 20,
		},
		{
			Hash:      "1acc7f1fc0f1c72e10f178f86b7d369b",
			SizeBytes: 40,
		},
		{
			Hash:      "bad2614f186bf481ee339896089825b5",
			SizeBytes: 60,
		},
		{
			Hash:      "87a890520c755d7b5fd322f6e3c487e2",
			SizeBytes: 80,
		},
		{
			Hash:      "51656a4fad2e76ec95dd969d18e87994",
			SizeBytes: 100,
		},
		{
			Hash:      "e0fe265acd2314151b4b5954ec1f748d",
			SizeBytes: 130,
		},
		{
			Hash:      "1a77aacc1ed3ea410230d66f1238d5a8",
			SizeBytes: 150,
		},
		{
			Hash:      "6f2bf26893e588575985446bf9fd116e",
			SizeBytes: 170,
		},
		{
			Hash:      "4381b565d55c06d4021488ecaed98704",
			SizeBytes: 190,
		},
	}

	for _, tc := range []struct {
		desc      string
		blobs     []*rpb.Digest
		byteLimit int64
		wantSmall []*rpb.Digest
		wantLarge []*rpb.Digest
	}{
		{
			desc: "all small blobs",
			blobs: []*rpb.Digest{
				blobs[0],
				blobs[7],
				blobs[4],
				blobs[6],
				blobs[2],
				blobs[3],
				blobs[5],
				blobs[8],
				blobs[1],
			},
			byteLimit: 300,
			wantSmall: blobs,
			wantLarge: []*rpb.Digest{},
		},
		{
			desc: "all large blobs",
			blobs: []*rpb.Digest{
				blobs[6],
				blobs[0],
				blobs[1],
				blobs[2],
				blobs[7],
				blobs[4],
				blobs[5],
				blobs[8],
				blobs[3],
			},
			byteLimit: 40,
			wantSmall: []*rpb.Digest{},
			wantLarge: blobs,
		},
		{
			desc: "small and large blobs",
			blobs: []*rpb.Digest{
				blobs[5],
				blobs[3],
				blobs[7],
				blobs[1],
				blobs[2],
				blobs[4],
				blobs[0],
				blobs[6],
				blobs[8],
			},
			byteLimit: 150,
			wantSmall: blobs[:4],
			wantLarge: blobs[4:],
		},
	} {
		instance := "default"
		t.Run(tc.desc, func(t *testing.T) {
			small, large := separateBlobsByByteLimit(tc.blobs, instance, tc.byteLimit)
			if !protoEqual(small, tc.wantSmall) {
				t.Errorf("small=%q; want %q", small, tc.wantSmall)
			}
			if !protoEqual(large, tc.wantLarge) {
				t.Errorf("large=%q; want %q", large, tc.wantLarge)
			}
		})
	}
}

func TestUpload(t *testing.T) {
	// Blobs that are present in both local Store and file_server.
	presentBlobs := []*blobData{
		makeBlobData("5WGm1JJ1x77KSrlRgzxL"),
		makeBlobData("ZJ0BiCaayupcdD2nRTmXXrre772lCF"),
		makeBlobData("o2JzZO7qr6dwwR2CmXZtWDJ65ZkT885aruPAe0nm"),
	}

	// Blobs present on local Store but missing from file_server.
	missingFileBlobs := []*rpb.Digest{
		{
			Hash:      "1a77aacc1ed3ea410230d66f1238d5a8",
			SizeBytes: 50,
		}, {
			Hash:      "bad2614f186bf481ee339896089825b5",
			SizeBytes: 60,
		}, {
			Hash:      "6f2bf26893e588575985446bf9fd116e",
			SizeBytes: 70,
		},
	}

	// Blobs missing from local Store.
	missingStoreBlobs := []*rpb.Digest{
		{
			Hash:      "87a890520c755d7b5fd322f6e3c487e2",
			SizeBytes: 80,
		},
		{
			Hash:      "4381b565d55c06d4021488ecaed98704",
			SizeBytes: 90,
		},
		{
			Hash:      "51656a4fad2e76ec95dd969d18e87994",
			SizeBytes: 100,
		},
	}

	store := digest.NewStore()
	for _, blob := range presentBlobs {
		store.Set(makeFakeDigestData(blob.digest, blob.data))
	}
	for _, blob := range missingFileBlobs {
		store.Set(makeFakeDigestData(blob, nil))
	}

	toString := func(d *rpb.Digest) string {
		return fmt.Sprintf("%s/%d", d.Hash, d.SizeBytes)
	}

	for _, tc := range []struct {
		desc                    string
		blobs                   []*rpb.Digest
		byteLimit               int64
		wantStored              map[string][]byte
		wantMissing             []MissingBlob
		wantNumBatchUpdates     int
		wantNumByteStreamWrites int
	}{
		{
			desc:  "present blobs",
			blobs: getDigests(presentBlobs),
			wantStored: map[string][]byte{
				toString(presentBlobs[0].digest): presentBlobs[0].data,
				toString(presentBlobs[1].digest): presentBlobs[1].data,
				toString(presentBlobs[2].digest): presentBlobs[2].data,
			},
			wantNumBatchUpdates: 1,
		},
		{
			desc:  "missing blobs",
			blobs: concatDigests(missingStoreBlobs, missingFileBlobs),
			wantMissing: []MissingBlob{
				{
					Digest: missingFileBlobs[0],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingFileBlobs[1],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingFileBlobs[2],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingStoreBlobs[0],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingStoreBlobs[1],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingStoreBlobs[2],
					Err:    errBlobNotInReq,
				},
			},
			wantStored: map[string][]byte{},
		},
		{
			desc:  "present and missing blobs",
			blobs: concatDigests(missingFileBlobs, missingStoreBlobs, getDigests(presentBlobs)),
			wantStored: map[string][]byte{
				toString(presentBlobs[0].digest): presentBlobs[0].data,
				toString(presentBlobs[1].digest): presentBlobs[1].data,
				toString(presentBlobs[2].digest): presentBlobs[2].data,
			},
			wantMissing: []MissingBlob{
				{
					Digest: missingFileBlobs[0],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingFileBlobs[1],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingFileBlobs[2],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingStoreBlobs[0],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingStoreBlobs[1],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingStoreBlobs[2],
					Err:    errBlobNotInReq,
				},
			},
			wantNumBatchUpdates: 1,
		},
		{
			desc:      "present and missing blobs with limit > max blob size",
			blobs:     concatDigests(missingStoreBlobs, getDigests(presentBlobs), missingFileBlobs),
			byteLimit: 500,
			wantStored: map[string][]byte{
				toString(presentBlobs[0].digest): presentBlobs[0].data,
				toString(presentBlobs[1].digest): presentBlobs[1].data,
				toString(presentBlobs[2].digest): presentBlobs[2].data,
			},
			wantMissing: []MissingBlob{
				{
					Digest: missingFileBlobs[0],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingFileBlobs[1],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingFileBlobs[2],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingStoreBlobs[0],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingStoreBlobs[1],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingStoreBlobs[2],
					Err:    errBlobNotInReq,
				},
			},
			wantNumBatchUpdates: 1,
		},
		{
			desc:      "present and missing blobs with limit < max blob size",
			blobs:     concatDigests(missingStoreBlobs, missingFileBlobs, getDigests(presentBlobs)),
			byteLimit: 110,
			wantStored: map[string][]byte{
				toString(presentBlobs[0].digest): presentBlobs[0].data,
				toString(presentBlobs[1].digest): presentBlobs[1].data,
				toString(presentBlobs[2].digest): presentBlobs[2].data,
			},
			wantMissing: []MissingBlob{
				{
					Digest: missingFileBlobs[0],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingFileBlobs[1],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingFileBlobs[2],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingStoreBlobs[0],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingStoreBlobs[1],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingStoreBlobs[2],
					Err:    errBlobNotInReq,
				},
			},
			wantNumBatchUpdates:     1,
			wantNumByteStreamWrites: 2,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			instance := "instance"
			fc, err := newFakeCASClient(tc.byteLimit, instance)
			defer fc.teardown()
			if err != nil {
				t.Errorf("err=%q, want nil", err)
				return
			}

			cas := CAS{
				Client:            fc,
				Store:             store,
				CacheCapabilities: &rpb.CacheCapabilities{MaxBatchTotalSizeBytes: tc.byteLimit},
			}
			ctx := context.Background()
			sema := make(chan struct{}, 100)
			err = cas.Upload(ctx, instance, sema, tc.blobs...)

			if tc.wantMissing != nil {
				if missing, ok := err.(MissingError); ok {
					if !reflect.DeepEqual(missing.Blobs, tc.wantMissing) {
						t.Errorf("missing.Blobs=%q; want=%q", missing.Blobs, tc.wantMissing)
					}
				} else {
					t.Errorf("Unexpected error: %q", err)
				}
			} else if err != nil {
				t.Errorf("Unexpected error: %q", err)
			}

			casSrv := fc.server.cas
			if casSrv.BatchReqs() != tc.wantNumBatchUpdates {
				t.Errorf("casSrv.BatchReqs()=%d, want=%d", casSrv.BatchReqs(), tc.wantNumBatchUpdates)
			}
			if casSrv.WriteReqs() != tc.wantNumByteStreamWrites {
				t.Errorf("casSrv.WriteReqs()=%d, want=%d", casSrv.WriteReqs(), tc.wantNumByteStreamWrites)
			}

			stored := map[string][]byte{}
			for _, blob := range tc.blobs {
				data, ok := casSrv.Get(rdigest.Digest{
					Hash: blob.Hash,
					Size: blob.SizeBytes,
				})
				if ok {
					stored[toString(blob)] = data
				}
			}
			if !reflect.DeepEqual(stored, tc.wantStored) {
				t.Errorf("stored=%q; want=%q", stored, tc.wantStored)
			}
		})
	}
}

func toBatchReqs(bds []*blobData) []*rpb.BatchUpdateBlobsRequest_Request {
	var result []*rpb.BatchUpdateBlobsRequest_Request
	for _, bd := range bds {
		result = append(result, &rpb.BatchUpdateBlobsRequest_Request{
			Digest: bd.digest,
			Data:   bd.data,
		})
	}
	return result
}

func TestLookupBlobsInStore(t *testing.T) {
	// Blobs that are present in both local Store and file_server.
	presentBlobs := []*blobData{
		makeBlobData("5WGm1JJ1x77KSrlRgzxL"),
		makeBlobData("ZJ0BiCaayupcdD2nRTmXXrre772lCF"),
		makeBlobData("o2JzZO7qr6dwwR2CmXZtWDJ65ZkT885aruPAe0nm"),
	}
	// Blobs present on local Store but missing from file_server.
	missingFileBlobs := []*rpb.Digest{
		{
			Hash:      "1a77aacc1ed3ea410230d66f1238d5a8",
			SizeBytes: 50,
		},
		{
			Hash:      "bad2614f186bf481ee339896089825b5",
			SizeBytes: 60,
		},
		{
			Hash:      "6f2bf26893e588575985446bf9fd116e",
			SizeBytes: 70,
		},
	}
	// Blobs missing from local Store.
	missingStoreBlobs := []*rpb.Digest{
		{
			Hash:      "87a890520c755d7b5fd322f6e3c487e2",
			SizeBytes: 80,
		},
		{
			Hash:      "4381b565d55c06d4021488ecaed98704",
			SizeBytes: 90,
		},
		{
			Hash:      "51656a4fad2e76ec95dd969d18e87994",
			SizeBytes: 100,
		},
	}
	store := digest.NewStore()
	for _, blob := range presentBlobs {
		store.Set(makeFakeDigestData(blob.digest, blob.data))
	}
	for _, blob := range missingFileBlobs {
		store.Set(makeFakeDigestData(blob, nil))
	}
	for _, tc := range []struct {
		desc        string
		blobs       []*rpb.Digest
		wantReqs    []*rpb.BatchUpdateBlobsRequest_Request
		wantMissing []MissingBlob
	}{
		{
			desc:     "present blobs",
			blobs:    getDigests(presentBlobs),
			wantReqs: toBatchReqs(presentBlobs),
		},
		{
			desc:  "missing blobs",
			blobs: append(missingFileBlobs, missingStoreBlobs...),
			wantMissing: []MissingBlob{
				{
					Digest: missingFileBlobs[0],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingFileBlobs[1],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingFileBlobs[2],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingStoreBlobs[0],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingStoreBlobs[1],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingStoreBlobs[2],
					Err:    errBlobNotInReq,
				},
			},
		},
		{
			desc: "present and missing blobs",
			blobs: []*rpb.Digest{
				presentBlobs[0].digest,
				missingFileBlobs[0],
				missingStoreBlobs[0],
				presentBlobs[1].digest,
				missingFileBlobs[1],
				missingStoreBlobs[1],
				presentBlobs[2].digest,
				missingFileBlobs[2],
				missingStoreBlobs[2],
			},
			wantReqs: toBatchReqs(presentBlobs),
			wantMissing: []MissingBlob{
				{
					Digest: missingFileBlobs[0],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingStoreBlobs[0],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingFileBlobs[1],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingStoreBlobs[1],
					Err:    errBlobNotInReq,
				},
				{
					Digest: missingFileBlobs[2],
					Err:    errFakeSourceNotFound,
				},
				{
					Digest: missingStoreBlobs[2],
					Err:    errBlobNotInReq,
				},
			},
		},
	} {
		// Do test here
		t.Run(tc.desc, func(t *testing.T) {
			ctx := context.Background()
			sema := make(chan struct{}, 100)
			reqs, missing := lookupBlobsInStore(ctx, tc.blobs, store, sema)
			if !protoEqual(reqs, tc.wantReqs) {
				t.Errorf("reqs=%q; want %q", reqs, tc.wantReqs)
			}
			if !reflect.DeepEqual(missing, tc.wantMissing) {
				t.Errorf("missing=%q; want %q", missing, tc.wantMissing)
			}
		})
	}
}

func TestCreateBatchUpdateBlobsRequests(t *testing.T) {
	blobs := []*blobData{
		makeBlobData("2aMmqx86iH"),
		makeBlobData("5WGm1JJ1x77KSrlRgzxL"),
		makeBlobData("ZJ0BiCaayupcdD2nRTmXXrre772lCF"),
		makeBlobData("o2JzZO7qr6dwwR2CmXZtWDJ65ZkT885aruPAe0nm"),
		makeBlobData("q7cBg9I69ZiXwe1U883vSwLIXRZ2eGNUMD2gIeqSqWfLK9IYZh"),
		makeBlobData("iyBzGRoMAqpTEaseblU5wl9S2aub0tzhOpQYlwhDcRCQh32XSTOIueVN29mC"),
	}
	var blobReqs []*rpb.BatchUpdateBlobsRequest_Request
	for _, blob := range blobs {
		blobReqs = append(blobReqs, blobDataToBatchUpdateReq(blob))
	}

	// Large group of blobs for testing `batchBlobLimit`
	bigBlobReqs := make([]*rpb.BatchUpdateBlobsRequest_Request, batchBlobLimit+2, batchBlobLimit+2)
	for i := range bigBlobReqs {
		bigBlobReqs[i] = blobDataToBatchUpdateReq(blobs[i%len(blobs)])
	}

	instance := "instance"
	for _, tc := range []struct {
		desc      string
		reqs      []*rpb.BatchUpdateBlobsRequest_Request
		byteLimit int64
		want      []*rpb.BatchUpdateBlobsRequest
	}{
		{
			desc: "empty input",
		},
		{
			desc: "all blobs in one request",
			reqs: blobReqs,
			want: []*rpb.BatchUpdateBlobsRequest{
				{
					InstanceName: instance,
					Requests:     blobReqs,
				},
			},
		},
		{
			// Each blob, when added to a BatchUpdateBlobsRequest as the only element,
			// makes the BatchUpdateBlobsRequest proto size <= 150 bytes
			desc:      "limit 150 bytes",
			reqs:      blobReqs,
			byteLimit: 150,
			want: []*rpb.BatchUpdateBlobsRequest{
				{
					InstanceName: instance,
					Requests:     blobReqs[0:1],
				},
				{
					InstanceName: instance,
					Requests:     blobReqs[1:2],
				},
				{
					InstanceName: instance,
					Requests:     blobReqs[2:3],
				},
				{
					InstanceName: instance,
					Requests:     blobReqs[3:4],
				},
				{
					InstanceName: instance,
					Requests:     blobReqs[4:5],
				},
				{
					InstanceName: instance,
					Requests:     blobReqs[5:6],
				},
			},
		},
		{
			desc:      "limit 300 bytes",
			reqs:      blobReqs,
			byteLimit: 300,
			want: []*rpb.BatchUpdateBlobsRequest{
				{
					InstanceName: instance,
					Requests:     blobReqs[0:3],
				},
				{
					InstanceName: instance,
					Requests:     blobReqs[3:5],
				},
				{
					InstanceName: instance,
					Requests:     blobReqs[5:6],
				},
			},
		},
		{
			desc:      "limit 500 bytes",
			reqs:      blobReqs,
			byteLimit: 500,
			want: []*rpb.BatchUpdateBlobsRequest{
				{
					InstanceName: instance,
					Requests:     blobReqs[0:4],
				},
				{
					InstanceName: instance,
					Requests:     blobReqs[4:6],
				},
			},
		},
		{
			desc: "blob count limit",
			reqs: bigBlobReqs,
			want: []*rpb.BatchUpdateBlobsRequest{
				{
					InstanceName: instance,
					Requests:     bigBlobReqs[0:batchBlobLimit],
				},
				{
					InstanceName: instance,
					Requests:     bigBlobReqs[batchBlobLimit:],
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			batchReqs := createBatchUpdateBlobsRequests(tc.reqs, instance, tc.byteLimit)
			if !cmp.Equal(batchReqs, tc.want) {
				t.Errorf("batchReqs=%q; want %q", batchReqs, tc.want)
			}
		})
	}
}
