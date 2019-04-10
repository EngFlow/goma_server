// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package bytestreamio provides io interfaces on bytestream service.
package bytestreamio

import (
	"context"
	"errors"
	"fmt"
	"io"

	pb "google.golang.org/genproto/googleapis/bytestream"
)

// Exists checks resource identified by resourceName exists in bytestream server.
func Exists(ctx context.Context, c pb.ByteStreamClient, resourceName string) error {
	rd, err := c.Read(ctx, &pb.ReadRequest{
		ResourceName: resourceName,
		ReadLimit:    1,
	})
	if err != nil {
		return err
	}
	_, err = rd.Recv()
	return err
}

// Open opens reader on bytestream for resourceName.
// ctx will be used until Reader is closed.
func Open(ctx context.Context, c pb.ByteStreamClient, resourceName string) (*Reader, error) {
	rd, err := c.Read(ctx, &pb.ReadRequest{
		ResourceName: resourceName,
	})
	if err != nil {
		return nil, err
	}
	return &Reader{
		rd: rd,
	}, nil
}

// Reader is a reader on bytestream.
type Reader struct {
	rd   pb.ByteStream_ReadClient
	buf  []byte
	size int64
}

// Read reads data from bytestream.
// The maximum data chunk size would be determined by server side.
func (r *Reader) Read(buf []byte) (int, error) {
	if r.rd == nil {
		return 0, errors.New("bad Reader")
	}
	if len(r.buf) > 0 {
		n := copy(buf, r.buf)
		r.buf = r.buf[n:]
		r.size += int64(n)
		return n, nil
	}
	resp, err := r.rd.Recv()
	if err != nil {
		return 0, err
	}
	// resp.Data may be empty.
	// TODO: better to fill buf?
	r.buf = resp.Data
	n := copy(buf, r.buf)
	r.buf = r.buf[n:]
	r.size += int64(n)
	return n, nil
}

// Size reports read size by Read.
func (r *Reader) Size() int64 {
	return r.size
}

// Create creates writer on bytestream for resourceName.
// ctx will be used until Writer is closed.
func Create(ctx context.Context, c pb.ByteStreamClient, resourceName string) (*Writer, error) {
	wr, err := c.Write(ctx)
	if err != nil {
		return nil, err
	}
	return &Writer{
		resname: resourceName,
		wr:      wr,
	}, nil
}

// Writer is a writer on bytestream.
type Writer struct {
	resname string
	wr      pb.ByteStream_WriteClient
	offset  int64

	// bytestream will accept blobs by partial upload if the same
	// blobs are already uploaded by io.EOF of Send.
	// then, we don't need to Send rest of data, so Write just returns
	// success.  Close issues CloseAndRecv and don't check offset.
	ok bool
}

// Write writes data to bytestream.
// The maximum data chunk size would be determined by server side,
// so don't pass larger chunk than maximum data chunk size.
func (w *Writer) Write(buf []byte) (int, error) {
	if w.wr == nil {
		return 0, errors.New("bad Writer")
	}
	if w.ok {
		return len(buf), nil
	}
	err := w.wr.Send(&pb.WriteRequest{
		ResourceName: w.resname,
		WriteOffset:  w.offset,
		Data:         buf,
	})
	if err == io.EOF {
		// the blob already stored in CAS.
		w.ok = true
		return len(buf), nil
	}
	if err != nil {
		return 0, err
	}
	w.offset += int64(len(buf))
	return len(buf), nil
}

// Close cloes the writer.
func (w *Writer) Close() error {
	if w.wr == nil {
		return errors.New("bad Writer")
	}
	if w.ok {
		w.wr.CloseAndRecv()
		return nil
	}
	// The service will not view the resource as 'complete'
	// until the client has sent a 'WriteRequest' with 'finish_write'
	// set to 'true'.
	err := w.wr.Send(&pb.WriteRequest{
		ResourceName: w.resname,
		WriteOffset:  w.offset,
		FinishWrite:  true,
		// The client may leave 'data' empty.
	})
	if err != nil {
		return err
	}
	resp, err := w.wr.CloseAndRecv()
	if err != nil {
		return err
	}
	if resp.CommittedSize != w.offset {
		return fmt.Errorf("upload committed size %d != offset %d", resp.CommittedSize, w.offset)
	}
	return nil
}
