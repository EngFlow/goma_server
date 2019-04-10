// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package httprpc

import (
	"compress/flate"
	"compress/gzip"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/protobuf/proto"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	pb "go.chromium.org/goma/server/proto/auth"
)

func TestSeralizeToResponseWriterDeflate(t *testing.T) {
	want := &pb.AuthResp{
		Email: "goma-dev@google.com",
	}
	rw := httptest.NewRecorder()
	_, err := serializeToResponseWriter(context.Background(), rw, want, encodingDeflate)
	if err != nil {
		t.Errorf("serializeToResponseWriter()=_, %v; want=_, nil", err)
	}
	gotBytes, err := ioutil.ReadAll(flate.NewReader(rw.Result().Body))
	if err != nil {
		t.Errorf("serialize response read: %v", err)
	}
	got := &pb.AuthResp{}
	err = proto.Unmarshal(gotBytes, got)
	if err != nil {
		t.Errorf("unmarshal: %v", err)
	}
	if !proto.Equal(got, want) {
		t.Errorf("got %#v; want %#v", got, want)
	}
}

func TestSeralizeToResponseWriterGzip(t *testing.T) {
	want := &pb.AuthResp{
		Email: "goma-dev@google.com",
	}
	rw := httptest.NewRecorder()
	_, err := serializeToResponseWriter(context.Background(), rw, want, encodingGzip)
	if err != nil {
		t.Errorf("serializeToResponseWriter()=_, %v; want=_, nil", err)
	}
	r, err := gzip.NewReader(rw.Result().Body)
	if err != nil {
		t.Errorf("gzip %v", err)
	}
	gotBytes, err := ioutil.ReadAll(r)
	if err != nil {
		t.Errorf("serialize response read: %v", err)
	}
	got := &pb.AuthResp{}
	err = proto.Unmarshal(gotBytes, got)
	if err != nil {
		t.Errorf("unmarshal: %v", err)
	}
	if !proto.Equal(got, want) {
		t.Errorf("got %#v; want %#v", got, want)
	}
}

func TestHandler(t *testing.T) {
	var opts []HandlerOption

	handler := Handler(
		"Health",
		&healthpb.HealthCheckRequest{}, &healthpb.HealthCheckResponse{},
		func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return &healthpb.HealthCheckResponse{}, nil
		}, opts...)

	s := httptest.NewServer(handler)
	defer s.Close()

	_, err := http.Get(s.URL)
	if err != nil {
		t.Errorf("http.Get err: %v", err)
	}
}
