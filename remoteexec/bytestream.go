// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"context"
	"io"
	"path"

	"github.com/google/uuid"
	"go.opencensus.io/trace"
	pb "google.golang.org/genproto/googleapis/bytestream"
)

// ByteStream is a proxy that reads/writes data to/from a server, as defined in
// googleapis/bytestream. In the context of the exec server, it accesses a resource on the
// RBE server, specifically a resource under an RBE instance.
type ByteStream struct {
	// Adapter provides an interface to the RBE API.
	Adapter *Adapter

	// The name of the RBE instance, e.g. "projects/$PROJECT/instances/default_instance"
	InstanceName string
}

// Read proxies bytestream Read stream.
func (bs *ByteStream) Read(req *pb.ReadRequest, s pb.ByteStream_ReadServer) error {
	ctx := s.Context()
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/remoteexec.Adapter.Read")
	defer span.End()

	ctx = bs.Adapter.outgoingContext(ctx, nil)

	rd, err := bs.Adapter.client(ctx).ByteStream().Read(ctx, &pb.ReadRequest{
		ResourceName: path.Join(bs.InstanceName, req.ResourceName),
		ReadOffset:   req.ReadOffset,
		ReadLimit:    req.ReadLimit,
	})
	if err != nil {
		return err
	}
	for {
		resp, err := rd.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		err = s.Send(resp)
		if err != nil {
			return err
		}
	}
	return nil
}

// Write proxies bytestream Write stream.
func (bs *ByteStream) Write(s pb.ByteStream_WriteServer) error {
	ctx := s.Context()
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/remoteexec.Adapter.Write")
	defer span.End()

	ctx = bs.Adapter.outgoingContext(ctx, nil)
	uuid := uuid.New()
	wr, err := bs.Adapter.client(ctx).ByteStream().Write(ctx)
	if err != nil {
		return err
	}
	for {
		req, err := s.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			wr.CloseAndRecv()
			return nil
		}
		err = wr.Send(&pb.WriteRequest{
			ResourceName: path.Join(bs.InstanceName, "uploads", uuid.String(), req.ResourceName),
			WriteOffset:  req.WriteOffset,
			FinishWrite:  req.FinishWrite,
			Data:         req.Data,
		})
		if err != nil {
			wr.CloseAndRecv()
			return err
		}
	}
	resp, err := wr.CloseAndRecv()
	if err != nil {
		return err
	}
	return s.SendAndClose(resp)
}

// Write proxies bytestream QueryWriteStatus call.
func (bs *ByteStream) QueryWriteStatus(ctx context.Context, req *pb.QueryWriteStatusRequest) (*pb.QueryWriteStatusResponse, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/remoteexec.Adapter.QueryWriteStatus")
	defer span.End()

	ctx = bs.Adapter.outgoingContext(ctx, nil)

	return bs.Adapter.client(ctx).ByteStream().QueryWriteStatus(ctx, &pb.QueryWriteStatusRequest{
		ResourceName: path.Join(bs.InstanceName, req.ResourceName),
	})
}
