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
	"math/rand"
	"path"
	"testing"
	"time"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/bytestreamio"
	"go.chromium.org/goma/server/remoteexec/cas"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/rpc/grpctest"
)

func setup(rbe *fakeRBE) (client Client, stop func(), err error) {
	srv := grpc.NewServer()
	registerFakeRBE(srv, rbe)
	addr, serverStop, err := grpctest.StartServer(srv)
	if err != nil {
		return Client{}, func() {}, err
	}
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		serverStop()
		return Client{}, func() {}, err
	}
	return Client{
			ClientConn: conn,
		}, func() {
			conn.Close()
			serverStop()
		}, nil
}

func TestFakeActionCache(t *testing.T) {
	rbe := newFakeRBE()
	client, stop, err := setup(rbe)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	actionDigest := digest.Bytes("dummy-action-digest", []byte("dummy-action-digest"))
	req := &rpb.GetActionResultRequest{
		ActionDigest: actionDigest.Digest(),
	}

	req.InstanceName = "bad-instance"
	t.Logf(`GetActionResult(instance_name=%q)`, req.InstanceName)
	resp, err := client.GetActionResult(ctx, req)
	if got, want := status.Convert(err).Code(), codes.PermissionDenied; got != want {
		t.Errorf(`GetActionResult(ctx, req)=%v, %v; want %v`, resp, err, want)
	}

	req.InstanceName = path.Join(rbe.instancePrefix, "default_instance")
	t.Logf(`GetActionResult(instance_name=%q, no action digest)`, req.InstanceName)
	resp, err = client.GetActionResult(ctx, req)
	if got, want := status.Convert(err).Code(), codes.NotFound; got != want {
		t.Errorf(`GetActionResult(ctx, req)=%v, %v; want %v`, resp, err, want)
	}

	result := &rpb.ActionResult{
		StdoutDigest: digest.Bytes("stdout", []byte("ok")).Digest(),
	}
	rbe.cache.Set(actionDigest.Digest(), result)

	t.Logf(`GetActionResult(instance_name=%q, %s)`, req.InstanceName, cas.ResName("", actionDigest.Digest()))
	resp, err = client.GetActionResult(ctx, req)
	if err != nil || !proto.Equal(resp, result) {
		t.Errorf(`GetActionResult(ct, req)=%v, %v; want %v, nil`, resp, err, result)
	}
}

func TestFakeCAS(t *testing.T) {
	rbe := newFakeRBE()
	client, stop, err := setup(rbe)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	largeFile := make([]byte, 6*1024*1024)
	rand.Read(largeFile)
	data := []digest.Data{
		digest.Bytes("data0", []byte("data0")),
		digest.Bytes("data1", []byte("data1")),
		digest.Bytes("data2", []byte("data2")),
		digest.Bytes("large-data", largeFile),
	}
	var digests []*rpb.Digest
	for _, d := range data {
		digests = append(digests, d.Digest())
	}

	fmbReq := &rpb.FindMissingBlobsRequest{
		BlobDigests: digests,
	}

	t.Logf(`FindMissingBlobs(instance_name="bad-instance")`)
	fmbReq.InstanceName = "bad-instance"
	fmbResp, err := client.FindMissingBlobs(ctx, fmbReq)
	if got, want := status.Convert(err).Code(), codes.PermissionDenied; got != want {
		t.Errorf(`FindMissingBlobs(ctx, req)=%v, %v; want %v`, fmbResp, err, want)
	}

	fmbReq.InstanceName = path.Join(rbe.instancePrefix, "default_instance")
	t.Logf(`FindMissingBlobs(instance_name=%q)`, fmbReq.InstanceName)
	fmbResp, err = client.FindMissingBlobs(ctx, fmbReq)
	if err != nil || !proto.Equal(fmbResp, &rpb.FindMissingBlobsResponse{
		MissingBlobDigests: digests,
	}) {
		t.Errorf(`FindMissingBlobs(ctx, req)=%v, %v; want %v, nil`, fmbResp, err, digests)
	}

	bubReq := &rpb.BatchUpdateBlobsRequest{}
	var updates []*rpb.Digest
	var noUpdates []*rpb.Digest
	for i, d := range data {
		if i%2 == 1 {
			noUpdates = append(noUpdates, d.Digest())
			continue
		}
		rd, err := d.Open(ctx)
		if err != nil {
			t.Fatalf("data[%d].Open()=%v, %v", i, rd, err)
		}
		buf, err := ioutil.ReadAll(rd)
		rd.Close()
		req := &rpb.BatchUpdateBlobsRequest_Request{
			Digest: d.Digest(),
			Data:   buf,
		}
		bubReq.Requests = append(bubReq.Requests, req)
		updates = append(updates, d.Digest())
	}
	updateResult := func(resp *rpb.BatchUpdateBlobsResponse) (stored, failed []*rpb.Digest) {
		for _, r := range resp.Responses {
			err := status.FromProto(r.Status).Err()
			if err != nil {
				failed = append(failed, r.Digest)
				continue
			}
			stored = append(stored, r.Digest)
		}
		return stored, failed
	}

	t.Logf(`BatchUpdateBlobs(instance_name="bad-instance")`)
	bubReq.InstanceName = "bad-instance"
	bubResp, err := client.BatchUpdateBlobs(ctx, bubReq)
	if got, want := status.Convert(err).Code(), codes.PermissionDenied; got != want {
		t.Errorf(`BatchUpdateBlobs(ctx, req)=%v, %v; want %v`, bubResp, err, want)
	}

	instanceName := path.Join(rbe.instancePrefix, "default_instance")
	t.Logf(`BatchUpdateBlobs(instance_name=%q, %v)`, instanceName, bubReq)
	bubReq.InstanceName = instanceName
	bubResp, err = client.BatchUpdateBlobs(ctx, bubReq)
	stored, failed := updateResult(bubResp)
	if err != nil || !cmp.Equal(stored, updates, cmp.Comparer(proto.Equal)) || len(failed) > 0 {
		t.Errorf(`BatchUpdateBlobs(ctx, req)=%v, %v; want=%v, nil`, bubResp, err, updates)
	}

	t.Logf(`FindMissingBlobs(instance_name=%q) after update`, instanceName)
	fmbResp, err = client.FindMissingBlobs(ctx, fmbReq)
	if err != nil || !proto.Equal(fmbResp, &rpb.FindMissingBlobsResponse{
		MissingBlobDigests: noUpdates,
	}) {
		t.Errorf(`FindMissingBlobs(ctx, req)=%v, %v; want %v, nil`, fmbResp, err, digests)
	}

	t.Logf(`bytestream check`)
	resname := "bad-resource-name"
	err = bytestreamDownloadCheck(ctx, client, resname, data[0])
	if got, want := status.Convert(err).Code(), codes.PermissionDenied; got != want {
		t.Errorf(`bytestreamDownloadCheck(ctx, client, %q, data[0])=%v; want %v`, resname, err, want)
	}

	resname = path.Join(instanceName, "blobs/bad/num")
	err = bytestreamDownloadCheck(ctx, client, resname, data[0])
	if got, want := status.Convert(err).Code(), codes.InvalidArgument; got != want {
		t.Errorf(`bytestreamDownloadCheck(ctx, client, %q, data[0])=%v; want %v`, resname, err, want)
	}

	resname = cas.ResName(instanceName, data[0].Digest())
	err = bytestreamDownloadCheck(ctx, client, resname, data[0])
	if err != nil {
		t.Errorf(`bytestreamDownloadCheck(ctx, client, %q, data[0])=%v; want %v`, resname, err, nil)
	}

	resname = cas.ResName(instanceName, data[1].Digest())
	err = bytestreamDownloadCheck(ctx, client, resname, data[1])
	if got, want := status.Convert(err).Code(), codes.NotFound; got != want {
		t.Errorf(`bytestreamDownloadCheck(ctx, client, %q, data[1])=%v; want %v`, resname, err, want)
	}

	resname = cas.ResName(instanceName, data[2].Digest())
	err = bytestreamDownloadCheck(ctx, client, resname, data[2])
	if err != nil {
		t.Errorf(`bytestreamDownloadCheck(ctx, client, %q, data[2])=%v; want %v`, resname, err, nil)
	}

	resname = cas.ResName(instanceName, data[3].Digest())
	err = bytestreamDownloadCheck(ctx, client, resname, data[3])
	if got, want := status.Convert(err).Code(), codes.NotFound; got != want {
		t.Errorf(`bytestreamDownloadCheck(ctx, client, %q, data[3])=%v; want %v`, resname, err, want)
	}

	resname = "bad-resource-name"
	t.Logf(`bytestreamio.Create %q`, resname)
	err = bytestreamUpload(ctx, client.ByteStream(), resname, data[1])
	if got, want := status.Convert(err).Code(), codes.PermissionDenied; got != want {
		t.Errorf(`bytestreamUpload(ctx, client, %q, %v)=%v; want %v`, resname, data[1], err, want)
	}

	resname = path.Join(instanceName, "blobs/bad/num")
	t.Logf(`bytestreamio.Create %q`, resname)
	err = bytestreamUpload(ctx, client.ByteStream(), resname, data[1])
	if got, want := status.Convert(err).Code(), codes.InvalidArgument; got != want {
		t.Errorf(`bytestreamUpload(ctx, client, %q, %v)=%v; want %v`, resname, data[1], err, want)
	}

	resname = cas.UploadResName(instanceName, data[1].Digest())
	t.Logf(`bytestreamUpload %q`, resname)
	err = bytestreamUpload(ctx, client.ByteStream(), resname, data[1])
	if err != nil {
		t.Errorf(`bytestreamUpload(ctx, client, %q, %v)=%v; want nil error`, resname, data[1], err)
	}

	t.Logf(`bytestreamUpload %q again`, resname)
	err = bytestreamUpload(ctx, client.ByteStream(), resname, data[1])
	if err != nil {
		t.Errorf(`2nd bytestreamUpload(ctx, client, %q, %v)=%v; want nil error`, resname, data[1], err)
	}

	resname = cas.UploadResName(instanceName, data[1].Digest())
	t.Logf(`bytestreamUpload %q`, resname)
	err = bytestreamUpload(ctx, client.ByteStream(), resname, data[1])
	if err != nil {
		t.Errorf(`bytestreamUpload(ctx, client, %q, %v)=%v; want nil error`, resname, data[1], err)
	}

	resname = cas.UploadResName(instanceName, data[3].Digest())
	t.Logf(`bytestreamUpload %q`, resname)
	err = bytestreamUpload(ctx, client.ByteStream(), resname, data[3])
	if err != nil {
		t.Errorf(`bytestreamUpload(ctx, client, %q, %v)=%v; want nil error`, resname, data[3], err)
	}

	resname = cas.ResName(instanceName, data[1].Digest())
	err = bytestreamDownloadCheck(ctx, client, resname, data[1])
	if err != nil {
		t.Errorf(`bytestreamDownloadCheck(ctx, client, %q, data[1])=%v; want %v`, resname, err, nil)
	}

	resname = cas.ResName(instanceName, data[3].Digest())
	err = bytestreamDownloadCheck(ctx, client, resname, data[3])
	if err != nil {
		t.Errorf(`bytestreamDownloadCheck(ctx, client, %q, data[3])=%v; want %v`, resname, err, nil)
	}

	t.Logf(`FindMissingBlobs(instance_name=%q)`, instanceName)
	fmbReq.InstanceName = instanceName
	fmbResp, err = client.FindMissingBlobs(ctx, fmbReq)
	if err != nil || !proto.Equal(fmbResp, &rpb.FindMissingBlobsResponse{}) {
		t.Errorf(`FindMissingBlobs(ctx, req)=%v, %v; want %v, nil`, fmbResp, err, digests)
	}
}

func bytestreamDownloadCheck(ctx context.Context, client bpb.ByteStreamClient, resname string, data digest.Data) error {
	r, err := data.Open(ctx)
	if err != nil {
		return err
	}
	defer r.Close()
	var dbuf bytes.Buffer
	_, err = io.Copy(&dbuf, r)
	if err != nil {
		return err
	}

	rd, err := bytestreamio.Open(ctx, client, resname)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	_, err = io.Copy(&buf, rd)
	if err != nil {
		return err
	}

	if !cmp.Equal(dbuf.Bytes(), buf.Bytes()) {
		return fmt.Errorf("digest content mismatch for %s", resname)
	}
	return nil
}

func bytestreamUpload(ctx context.Context, client bpb.ByteStreamClient, resname string, data digest.Data) error {
	rd, err := data.Open(ctx)
	if err != nil {
		return err
	}
	defer rd.Close()
	wr, err := bytestreamio.Create(ctx, client, resname)
	if err != nil {
		return err
	}
	_, err = io.Copy(wr, rd)
	if err != nil {
		wr.Close()
		return err
	}
	return wr.Close()
}

func mustDigestProto(t *testing.T, m proto.Message) digest.Data {
	t.Helper()
	data, err := digest.Proto(m)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFakeExecute(t *testing.T) {
	rbe := newFakeRBE()
	wantResp := proto.Clone(rbe.execResp).(*rpb.ExecuteResponse)

	client, stop, err := setup(rbe)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	commandData := mustDigestProto(t, &rpb.Command{
		Arguments: []string{"echo", "hello, world"},
		Platform:  &rpb.Platform{},
	})
	helloData := digest.Bytes("hello.txt", []byte("hello"))
	rbe.cas.Set(helloData)
	inputRootData := mustDigestProto(t, &rpb.Directory{
		Files: []*rpb.FileNode{
			{
				Name:   "hello.txt",
				Digest: helloData.Digest(),
			},
		},
	})

	actionData := mustDigestProto(t, &rpb.Action{})
	rbe.cas.Set(actionData)

	req := &rpb.ExecuteRequest{
		ActionDigest: actionData.Digest(),
	}

	req.InstanceName = "bad-instance"
	t.Logf(`Execute(instance_name=%q)`, req.InstanceName)
	opname, resp, err := ExecuteAndWait(ctx, client, req)
	if got, want := status.Convert(err).Code(), codes.PermissionDenied; got != want {
		t.Errorf(`ExecuteAndWait(ctx, client, req)=%q, %v, %v; want %v`, opname, resp, err, want)
	}

	req.InstanceName = path.Join(rbe.instancePrefix, "default_instance")
	t.Logf(`Execute(instance_name=%q)`, req.InstanceName)
	opname, resp, err = ExecuteAndWait(ctx, client, req)
	if got, want := status.Convert(err).Code(), codes.InvalidArgument; got != want {
		t.Errorf(`ExecuteAndWait(ctx, client, req)=%q, %v, %v; want %v`, opname, resp, err, want)
	}

	t.Logf(`set command digest`)
	actionData = mustDigestProto(t, &rpb.Action{
		CommandDigest: commandData.Digest(),
	})
	rbe.cas.Set(actionData)
	req.ActionDigest = actionData.Digest()
	opname, resp, err = ExecuteAndWait(ctx, client, req)
	if got, want := status.Convert(err).Code(), codes.InvalidArgument; got != want {
		t.Errorf(`ExecuteAndWait(ctx, client, req)=%q, %v, %v; want %v`, opname, resp, err, want)
	}

	t.Logf(`store command data`)
	rbe.cas.Set(commandData)
	opname, resp, err = ExecuteAndWait(ctx, client, req)
	if got, want := status.Convert(err).Code(), codes.InvalidArgument; got != want {
		t.Errorf(`ExecuteAndWait(ctx, client, req)=%q, %v, %v; want %v`, opname, resp, err, want)
	}

	t.Logf(`set input root digest`)
	actionData = mustDigestProto(t, &rpb.Action{
		CommandDigest:   commandData.Digest(),
		InputRootDigest: inputRootData.Digest(),
	})
	rbe.cas.Set(actionData)
	req.ActionDigest = actionData.Digest()
	opname, resp, err = ExecuteAndWait(ctx, client, req)
	if got, want := status.Convert(err).Code(), codes.InvalidArgument; got != want {
		t.Errorf(`ExecuteAndWait(ctx, client, req)=%q, %v, %v; want %v`, opname, resp, err, want)
	}

	t.Logf(`store input_root data`)
	rbe.cas.Set(inputRootData)
	opname, resp, err = ExecuteAndWait(ctx, client, req)
	if err != nil || !proto.Equal(resp, wantResp) {
		t.Errorf(`ExecuteAndWait(ctx, client, req)=%q, %v, %v; want %v, nil error`, opname, resp, err, wantResp)
	}

	t.Logf(`Execute(instance_name=%q) cached`, req.InstanceName)
	opname, resp, err = ExecuteAndWait(ctx, client, req)
	if err != nil || !proto.Equal(resp.Result, wantResp.Result) {
		t.Errorf(`ExecuteAndWait(ctx, client, req)=%q, %v, %v; want %v, nil error`, opname, resp, err, wantResp)
	}
	if !resp.CachedResult {
		t.Errorf(`resp.CachedResult=%t; want=true`, resp.CachedResult)
	}

	req.SkipCacheLookup = true
	t.Logf(`Execute(instance_name=%q) skip cache`, req.InstanceName)
	opname, resp, err = ExecuteAndWait(ctx, client, req)
	if err != nil || !proto.Equal(resp.Result, wantResp.Result) {
		t.Errorf(`ExecuteAndWait(ctx, client, req)=%q, %v, %v; want %v, nil error`, opname, resp, err, wantResp)
	}
}
