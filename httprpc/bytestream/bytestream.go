// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package bytestream implements bytestream for goma http.
package bytestream

import (
	"compress/gzip"
	"compress/zlib"
	"context"
	"io"
	"net/http"
	"path"
	"strings"

	pb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/bytestreamio"
	"go.chromium.org/goma/server/httprpc"
	"go.chromium.org/goma/server/log"
)

const bufsize = 2 * 1024 * 1024

// Handler returns http.Handler to serve bytestream API.
func Handler(c pb.ByteStreamClient, opts ...httprpc.HandlerOption) http.Handler {
	return httprpc.StreamHandler(
		"Bytestream",
		func(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
			logger := log.FromContext(ctx)
			switch r.Method {
			case http.MethodGet:
				return byteStreamGet(ctx, c, w, r)

			case http.MethodPost:
				return byteStreamPost(ctx, c, w, r)

			case http.MethodHead:
				return byteStreamHead(ctx, c, w, r)

			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			logger.Errorf("server error %s %s: %d %s", r.Method, r.URL.Path, http.StatusMethodNotAllowed, "method not allowed")
			return nil
		}, opts...)
}

func byteStreamResourceName(r *http.Request) string {
	return strings.TrimLeft(path.Clean(r.URL.Path), "/")
}

func byteStreamGet(ctx context.Context, c pb.ByteStreamClient, w http.ResponseWriter, r *http.Request) error {
	resname := byteStreamResourceName(r)

	rd, err := bytestreamio.Open(ctx, c, resname)
	if err != nil {
		return err
	}
	buf := make([]byte, bufsize)
	var wr io.Writer = w
	switch {
	case strings.Contains(r.Header.Get("Accept-Encoding"), "gzip"):
		wr, err = gzip.NewWriterLevel(w, gzip.BestSpeed)
		if err != nil {
			return err
		}
		w.Header().Set("Content-Encoding", "gzip")

	case strings.Contains(r.Header.Get("Accept-Encoding"), "deflate"):
		// RFC7230 says deflate coding is "zlib" (RFC1950) containing
		// "deflate" compressed data (RFC1951).
		// but goma client just used "deflate" compressed data
		// for "Content-Encoding: deflate" wrongly.
		wr, err = zlib.NewWriterLevel(w, zlib.BestSpeed)
		if err != nil {
			return err
		}
		w.Header().Set("Content-Encoding", "deflate")
	}
	_, err = io.CopyBuffer(wr, rd, buf)
	if err != nil {
		return err
	}
	if c, ok := wr.(io.Closer); ok {
		err = c.Close()
	}
	return err
}

func byteStreamPost(ctx context.Context, c pb.ByteStreamClient, w http.ResponseWriter, r *http.Request) error {
	resname := byteStreamResourceName(r)
	wr, err := bytestreamio.Create(ctx, c, resname)
	if err != nil {
		return err
	}
	buf := make([]byte, bufsize)
	var rd io.Reader = r.Body
	switch r.Header.Get("Content-Encoding") {
	case "gzip":
		gr, err := gzip.NewReader(r.Body)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "gzip error: %v", err)
		}
		defer gr.Close()
		rd = gr

	case "deflate":
		// RFC7230 says deflate coding is "zlib" (RFC1950) containing
		// "deflate" compressed data (RFC1951).
		// but goma client just used "deflate" compressed data
		// for "Content-Encoding: deflate" wrongly.
		rd, err = zlib.NewReader(r.Body)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "zlib error: %v", err)
		}
	}
	_, err = io.CopyBuffer(wr, rd, buf)
	if err != nil {
		wr.Close()
		return err
	}
	err = wr.Close()
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

func byteStreamHead(ctx context.Context, c pb.ByteStreamClient, w http.ResponseWriter, r *http.Request) error {
	resname := byteStreamResourceName(r)

	err := bytestreamio.Exists(ctx, c, resname)
	if err != nil {
		return err
	}
	// client doesn't handle StatusOK well.
	// got "connection closed before receiving all chunks".
	w.WriteHeader(http.StatusNoContent)
	return nil
}
