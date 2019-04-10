// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package bytestream

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeByteStreamClient struct {
	pb.ByteStreamClient
	m map[string]string
}

func (c fakeByteStreamClient) Read(ctx context.Context, req *pb.ReadRequest, opts ...grpc.CallOption) (pb.ByteStream_ReadClient, error) {
	v, ok := c.m[req.ResourceName]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s is not found", req.ResourceName)
	}
	if req.ReadOffset >= int64(len(v)) {
		return nil, status.Errorf(codes.OutOfRange, "out of range %d > %d", req.ReadOffset, len(v))
	}
	v = v[req.ReadOffset:]
	if req.ReadLimit < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "negative limit %d", req.ReadLimit)
	}
	if req.ReadLimit > 0 && int64(len(v)) > req.ReadLimit {
		v = v[:req.ReadLimit]
	}
	return &fakeByteStreamReadClient{
		c:    c,
		data: v,
	}, nil
}

func (c fakeByteStreamClient) Write(ctx context.Context, opts ...grpc.CallOption) (pb.ByteStream_WriteClient, error) {
	return &fakeByteStreamWriteClient{
		c: c,
	}, nil
}

type fakeByteStreamReadClient struct {
	pb.ByteStream_ReadClient
	c    fakeByteStreamClient
	data string
}

func (c *fakeByteStreamReadClient) Recv() (*pb.ReadResponse, error) {
	if len(c.data) == 0 {
		return nil, io.EOF
	}
	const chunksize = 16
	v := c.data
	if len(v) > chunksize {
		v = v[:chunksize]
	}
	c.data = c.data[len(v):]
	return &pb.ReadResponse{
		Data: []byte(v),
	}, nil
}

type fakeByteStreamWriteClient struct {
	pb.ByteStream_WriteClient
	c fakeByteStreamClient

	resname string
	buf     *bytes.Buffer
	finish  bool
}

func (c *fakeByteStreamWriteClient) Send(req *pb.WriteRequest) error {
	if c.buf == nil {
		c.buf = bytes.NewBuffer(nil)
		c.resname = req.ResourceName
		if c.resname == "" {
			return status.Errorf(codes.InvalidArgument, "empty resname")
		}
	} else if req.ResourceName != "" && req.ResourceName != c.resname {
		return status.Errorf(codes.InvalidArgument, "resname mismatch %s; want=%s", req.ResourceName, c.resname)
	}
	_, ok := c.c.m[c.resname]
	if ok {
		return io.EOF
	}
	if req.WriteOffset != int64(c.buf.Len()) {
		return status.Errorf(codes.InvalidArgument, "incorrect offset %d; want=%d", req.WriteOffset, c.buf.Len())
	}
	if c.finish {
		return status.Errorf(codes.FailedPrecondition, "write to already finished resource")
	}
	c.finish = req.FinishWrite
	c.buf.Write(req.Data)
	return nil
}

func (c *fakeByteStreamWriteClient) CloseAndRecv() (*pb.WriteResponse, error) {
	if !c.finish {
		return nil, status.Errorf(codes.FailedPrecondition, "write not finished")
	}
	c.c.m[c.resname] = c.buf.String()
	return &pb.WriteResponse{
		CommittedSize: int64(c.buf.Len()),
	}, nil
}

func TestGet(t *testing.T) {
	const resname = `blobs/hash/size`
	const data = `blob data`
	c := fakeByteStreamClient{
		m: map[string]string{
			resname: data,
		},
	}
	handler := Handler(c)
	s := httptest.NewServer(handler)
	defer s.Close()

	req, err := http.NewRequest("GET", s.URL+"/"+resname, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET %s=%d %s; want=%d", resname, resp.StatusCode, resp.Status, http.StatusOK)
	}
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(buf), data; got != want {
		t.Errorf("GET %s=%q; want=%q", resname, got, want)
	}
}

func TestGetDeflate(t *testing.T) {
	const resname = `blobs/hash/size`
	const data = `blob data`
	c := fakeByteStreamClient{
		m: map[string]string{
			resname: data,
		},
	}
	handler := Handler(c)
	s := httptest.NewServer(handler)
	defer s.Close()

	req, err := http.NewRequest("GET", s.URL+"/"+resname, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "deflate")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET %s=%d %s; want=%d", resname, resp.StatusCode, resp.Status, http.StatusOK)
	}
	if got, want := resp.Header.Get("Content-Encoding"), "deflate"; got != want {
		t.Errorf("GET %s: content-encoding=%s; want=%s", resname, got, want)
	}
	rawbuf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := string(rawbuf), data; got == want {
		t.Errorf("GET %s=%q; should be compressed with deflate of %q", resname, got, want)
	}
	r, err := zlib.NewReader(bytes.NewReader(rawbuf))
	if err != nil {
		t.Errorf("GET %s; zlib %v", resname, err)
	}
	buf, err := ioutil.ReadAll(r)
	if err != nil {
		t.Fatalf("deflate %q=%q: %v", rawbuf, string(buf), err)
	}
	if got, want := string(buf), data; got != want {
		t.Errorf("GET %s=%q; want=%q", resname, got, want)
	}
}

func TestGetGzip(t *testing.T) {
	const resname = `blobs/hash/size`
	const data = `blob data`
	c := fakeByteStreamClient{
		m: map[string]string{
			resname: data,
		},
	}
	handler := Handler(c)
	s := httptest.NewServer(handler)
	defer s.Close()

	req, err := http.NewRequest("GET", s.URL+"/"+resname, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET %s=%d %s; want=%d", resname, resp.StatusCode, resp.Status, http.StatusOK)
	}
	if got, want := resp.Header.Get("Content-Encoding"), "gzip"; got != want {
		t.Errorf("GET %s: content-encoding=%s; want=%s", resname, got, want)
	}
	rawbuf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := string(rawbuf), data; got == want {
		t.Errorf("GET %s=%q; should be compressed with gzip of %q", resname, got, want)
	}
	r, err := gzip.NewReader(bytes.NewReader(rawbuf))
	if err != nil {
		t.Errorf("GET %s; gzip %v", resname, err)
	}
	buf, err := ioutil.ReadAll(r)
	if err != nil {
		t.Fatalf("gzip %q=%q: %v", rawbuf, string(buf), err)
	}
	if got, want := string(buf), data; got != want {
		t.Errorf("GET %s=%q; want=%q", resname, got, want)
	}
}

func TestGetNotFound(t *testing.T) {
	const resname = `blobs/hash/size`
	c := fakeByteStreamClient{
		m: map[string]string{},
	}
	handler := Handler(c)
	s := httptest.NewServer(handler)
	defer s.Close()

	req, err := http.NewRequest("GET", s.URL+"/"+resname, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET %s=%d %s; want=%d", resname, resp.StatusCode, resp.Status, http.StatusNotFound)
	}
}

func TestHead(t *testing.T) {
	const resname = `blobs/hash/size`
	const data = `blob data`
	c := fakeByteStreamClient{
		m: map[string]string{
			resname: data,
		},
	}
	handler := Handler(c)
	s := httptest.NewServer(handler)
	defer s.Close()

	req, err := http.NewRequest("HEAD", s.URL+"/"+resname, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("HEAD %s=%d %s; want=%d", resname, resp.StatusCode, resp.Status, http.StatusNoContent)
	}
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(buf), ""; got != want {
		t.Errorf("HEAD %s=%q; want=%q", resname, got, want)
	}
}

func TestHeadNotFound(t *testing.T) {
	const resname = `blobs/hash/size`
	c := fakeByteStreamClient{
		m: map[string]string{},
	}
	handler := Handler(c)
	s := httptest.NewServer(handler)
	defer s.Close()

	req, err := http.NewRequest("HEAD", s.URL+"/"+resname, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("HEAD %s=%d %s; want=%d", resname, resp.StatusCode, resp.Status, http.StatusNoContent)
	}
}

func TestPost(t *testing.T) {
	const resname = `blobs/hash/size`
	const data = `blob data`
	c := fakeByteStreamClient{
		m: map[string]string{},
	}
	handler := Handler(c)
	s := httptest.NewServer(handler)
	defer s.Close()

	req, err := http.NewRequest("POST", s.URL+"/"+resname, strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST %s=%d %s; want=%d", resname, resp.StatusCode, resp.Status, http.StatusOK)
	}
	got, ok := c.m[resname]
	if !ok {
		t.Errorf("POST %s didn't create data", resname)
	}
	if got != data {
		t.Errorf("POST %s=%q; want=%q", resname, got, data)
	}
}

func TestPostDeflate(t *testing.T) {
	const resname = `blobs/hash/size`
	const data = `blob data`
	c := fakeByteStreamClient{
		m: map[string]string{},
	}
	handler := Handler(c)
	s := httptest.NewServer(handler)
	defer s.Close()

	var buf bytes.Buffer
	w, err := zlib.NewWriterLevel(&buf, zlib.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Write([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", s.URL+"/"+resname, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Encoding", "deflate")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST %s=%d %s; want=%d", resname, resp.StatusCode, resp.Status, http.StatusOK)
	}
	got, ok := c.m[resname]
	if !ok {
		t.Errorf("POST %s didn't create data", resname)
	}
	if got != data {
		t.Errorf("POST %s=%q; want=%q", resname, got, data)
	}
}

func TestPostGzip(t *testing.T) {
	const resname = `blobs/hash/size`
	const data = `blob data`
	c := fakeByteStreamClient{
		m: map[string]string{},
	}
	handler := Handler(c)
	s := httptest.NewServer(handler)
	defer s.Close()

	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Write([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", s.URL+"/"+resname, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST %s=%d %s; want=%d", resname, resp.StatusCode, resp.Status, http.StatusOK)
	}
	got, ok := c.m[resname]
	if !ok {
		t.Errorf("POST %s didn't create data", resname)
	}
	if got != data {
		t.Errorf("POST %s=%q; want=%q", resname, got, data)
	}
}

func TestPostAlreadyExists(t *testing.T) {
	const resname = `blobs/hash/size`
	const data = `blob data`
	c := fakeByteStreamClient{
		m: map[string]string{
			resname: data,
		},
	}
	handler := Handler(c)
	s := httptest.NewServer(handler)
	defer s.Close()

	req, err := http.NewRequest("POST", s.URL+"/"+resname, strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST %s=%d %s; want=%d", resname, resp.StatusCode, resp.Status, http.StatusOK)
	}
	got, ok := c.m[resname]
	if !ok {
		t.Errorf("POST %s didn't create data", resname)
	}
	if got != data {
		t.Errorf("POST %s=%q; want=%q", resname, got, data)
	}
}

func TestBadMethod(t *testing.T) {
	const resname = `blobs/hash/size`
	c := fakeByteStreamClient{
		m: map[string]string{},
	}
	handler := Handler(c)
	s := httptest.NewServer(handler)
	defer s.Close()

	req, err := http.NewRequest("PUT", s.URL+"/"+resname, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("PUT %s=%d %s; want=%d", resname, resp.StatusCode, resp.Status, http.StatusMethodNotAllowed)
	}
}
