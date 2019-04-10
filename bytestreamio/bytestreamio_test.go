// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package bytestreamio

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/google/go-cmp/cmp"
	pb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
)

type stubByteStreamReadClient struct {
	pb.ByteStreamClient
	resourceName string
	data         []byte
	chunksize    int
}

func (c *stubByteStreamReadClient) Read(ctx context.Context, req *pb.ReadRequest, opts ...grpc.CallOption) (pb.ByteStream_ReadClient, error) {
	if req.ResourceName != c.resourceName {
		return nil, fmt.Errorf("bad resource name: %q; want %q", req.ResourceName, c.resourceName)
	}
	if req.ReadOffset != 0 {
		return nil, fmt.Errorf("bad read offset=%d; want=%d", req.ReadOffset, 0)
	}
	if req.ReadLimit != 0 {
		return nil, fmt.Errorf("bad read limit=%d; want=%d", req.ReadLimit, 0)
	}
	return &stubReadClient{
		c: c,
	}, nil
}

type stubReadClient struct {
	pb.ByteStream_ReadClient
	c      *stubByteStreamReadClient
	offset int
}

func (r *stubReadClient) Recv() (*pb.ReadResponse, error) {
	if r.offset >= len(r.c.data) {
		return nil, io.EOF
	}
	data := r.c.data[r.offset:]
	if len(data) > r.c.chunksize {
		data = data[:r.c.chunksize]
	}
	r.offset += len(data)
	return &pb.ReadResponse{
		Data: data,
	}, nil
}

func TestReader(t *testing.T) {
	const datasize = 1 * 1024 * 1024
	const chunksize = 8192
	const bufsize = 1024
	data := make([]byte, 4*1024*1024)
	_, err := rand.Read(data)
	if err != nil {
		t.Fatal(err)
	}

	const resourceName = "resource-name"
	c := &stubByteStreamReadClient{
		resourceName: resourceName,
		data:         data,
		chunksize:    chunksize,
	}
	ctx := context.Background()

	r, err := Open(ctx, c, resourceName)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if bytes.Equal(out.Bytes(), data) {
		t.Fatal("data setup failed")
	}

	buf := make([]byte, bufsize)
	_, err = io.CopyBuffer(&out, r, buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Errorf("read doesn't match: (-want +got)\n%s", cmp.Diff(data, out))
	}
}

type stubByteStreamWriteClient struct {
	pb.ByteStreamClient
	resourceName  string
	buf           bytes.Buffer
	chunksize     int
	alreadyExists bool
	finished      bool
}

func (c *stubByteStreamWriteClient) Write(ctx context.Context, opts ...grpc.CallOption) (pb.ByteStream_WriteClient, error) {
	return &stubWriteClient{
		c: c,
	}, nil
}

type stubWriteClient struct {
	pb.ByteStream_WriteClient
	c *stubByteStreamWriteClient
}

func (w *stubWriteClient) Send(req *pb.WriteRequest) error {
	if req.ResourceName != w.c.resourceName {
		return fmt.Errorf("bad resource name: %q; want %q", req.ResourceName, w.c.resourceName)
	}
	if w.c.finished {
		return errors.New("bad write to finished client")
	}
	if req.WriteOffset != int64(w.c.buf.Len()) {
		return fmt.Errorf("bad write offset=%d; want=%d", req.WriteOffset, w.c.buf.Len())
	}
	if req.FinishWrite {
		w.c.finished = true
	}
	if len(req.Data) > w.c.chunksize {
		return fmt.Errorf("too large data=%d. chunksize=%d", len(req.Data), w.c.chunksize)
	}
	w.c.buf.Write(req.Data) // err is always nil.
	if w.c.alreadyExists {
		return io.EOF
	}
	return nil
}

func (w *stubWriteClient) CloseAndRecv() (*pb.WriteResponse, error) {
	return &pb.WriteResponse{
		CommittedSize: int64(w.c.buf.Len()),
	}, nil
}

// to hite WriteTo.
type bytesReader struct {
	r *bytes.Reader
}

func (r bytesReader) Read(buf []byte) (int, error) {
	return r.r.Read(buf)
}

func TestWriter(t *testing.T) {
	const datasize = 1*1024*1024 + 2048
	const chunksize = 8192
	const bufsize = 1024

	data := make([]byte, 4*1024*1024)
	_, err := rand.Read(data)
	if err != nil {
		t.Fatal(err)
	}

	const resourceName = "resource-name"
	c := &stubByteStreamWriteClient{
		resourceName: resourceName,
		chunksize:    chunksize,
	}
	if bytes.Equal(c.buf.Bytes(), data) {
		t.Fatalf("data setup failed")
	}
	ctx := context.Background()

	w, err := Create(ctx, c, resourceName)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, bufsize)
	_, err = io.CopyBuffer(w, bytesReader{bytes.NewReader(data)}, buf)
	if err != nil {
		w.Close()
		t.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !c.finished {
		t.Errorf("write not finished")
	}
	if c.buf.Len() != len(data) {
		t.Errorf("write len=%d; want=%d", c.buf.Len(), len(data))
	}
	if !bytes.Equal(c.buf.Bytes(), data) {
		t.Errorf("write doesn't match: (-want +got)\n%s", cmp.Diff(data, c.buf.Bytes()))
	}

}

func TestWriterAlreadyExists(t *testing.T) {
	const datasize = 1*1024*1024 + 2048
	const chunksize = 8192
	const bufsize = 1024

	data := make([]byte, 4*1024*1024)
	_, err := rand.Read(data)
	if err != nil {
		t.Fatal(err)
	}

	const resourceName = "resource-name"
	c := &stubByteStreamWriteClient{
		resourceName:  resourceName,
		chunksize:     chunksize,
		alreadyExists: true,
	}
	if bytes.Equal(c.buf.Bytes(), data) {
		t.Fatalf("data setup failed")
	}
	ctx := context.Background()

	w, err := Create(ctx, c, resourceName)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, bufsize)
	_, err = io.CopyBuffer(w, bytesReader{bytes.NewReader(data)}, buf)
	if err != nil {
		w.Close()
		t.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !w.ok {
		t.Errorf("writer.ok=%t; want=true", w.ok)
	}
	if c.buf.Len() == len(data) {
		t.Errorf("write len=%d << %d", c.buf.Len(), len(data))
	}
	if bytes.Equal(c.buf.Bytes(), data) {
		t.Errorf("write match? should not match for already exists resource")
	}
}
