// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package httprpc

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/golang/protobuf/proto"
	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Client is httprpc client.
type Client struct {
	*http.Client

	// endpoint URL.
	URL string

	// ContentEncoding of the request. "identity", "gzip" or "deflate".
	// "deflate" uses "deflate" compressed data (RFC1951) without
	// zlib header, different from RFC7230 says, for histrical reason.
	// default is "deflate" for backward compatibility.
	// TODO: change default to gzip?
	ContentEncoding string
}

func serializeToHTTPRequest(ctx context.Context, url string, req proto.Message, contentEncoding string) (*http.Request, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/httprpc.serializedToHTTPRequest")
	defer span.End()
	reqMsg, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(nil)
	switch contentEncoding {
	case "identity":
		buf.Write(reqMsg)

	case "deflate":
		// RFC7230 says deflate coding is "zlib" (RFC1950) containing
		// "deflate" compressed data (RFC1951).
		// but goma client just used "deflate" compressed data
		// for "Content-Encoding: deflate" wrongly.
		// TODO: use gzip
		w, err := flate.NewWriter(buf, flate.BestSpeed)
		if err != nil {
			return nil, err
		}
		_, err = w.Write(reqMsg)
		if err != nil {
			return nil, err
		}
		err = w.Close()
		if err != nil {
			return nil, err
		}

	case "gzip":
		w, err := gzip.NewWriterLevel(buf, gzip.BestSpeed)
		if err != nil {
			return nil, err
		}
		_, err = w.Write(reqMsg)
		if err != nil {
			return nil, err
		}
		err = w.Close()
		if err != nil {
			return nil, err
		}
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unsupported content-encoding: %s", contentEncoding)
	}

	len := int64(len(buf.Bytes()))
	post, err := http.NewRequest("POST", url, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, err
	}
	post = post.WithContext(ctx)
	post.Header.Set("Content-Type", "binary/x-protocol-buffer")
	post.ContentLength = len
	post.Header.Set("Accept-Encoding", "gzip, deflate")
	post.Header.Set("Content-Encoding", contentEncoding)
	return post, nil
}

func fromHTTPStatus(code int) codes.Code {
	// go/http-canonical-mapping
	if code >= http.StatusOK && code < http.StatusMultipleChoices {
		return codes.OK
	}
	if code >= http.StatusMultipleChoices && code < http.StatusBadRequest {
		return codes.Unknown
	}
	switch code {
	case http.StatusBadRequest:
		return codes.InvalidArgument
	case http.StatusUnauthorized:
		return codes.Unauthenticated
	case http.StatusForbidden:
		return codes.PermissionDenied
	case http.StatusNotFound:
		return codes.NotFound
	case http.StatusConflict:
		return codes.Aborted
	case http.StatusRequestedRangeNotSatisfiable:
		return codes.OutOfRange
	case http.StatusTooManyRequests:
		return codes.ResourceExhausted
	case 499: // Client closed request
		return codes.Canceled
	case http.StatusNotImplemented:
		return codes.Unimplemented
	case http.StatusServiceUnavailable:
		return codes.Unavailable
	case http.StatusGatewayTimeout:
		return codes.DeadlineExceeded
	}
	if code >= http.StatusBadRequest && code < http.StatusInternalServerError {
		return codes.FailedPrecondition
	}
	if code >= http.StatusInternalServerError {
		return codes.Internal
	}
	return codes.Unknown
}

func parseFromHTTPResponse(ctx context.Context, response *http.Response, resp proto.Message) error {
	defer response.Body.Close()
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/httprpc.parseFromHTTPResponse")
	defer span.End()
	span.AddAttributes(trace.Int64Attribute("status_code", int64(response.StatusCode)))
	if response.StatusCode != 200 {
		span.AddAttributes(trace.StringAttribute("goma_error", response.Header.Get("X-Goma-Error")))
		b, err := ioutil.ReadAll(response.Body)

		return status.Errorf(fromHTTPStatus(response.StatusCode), "%d: %s: %s: %v", response.StatusCode, response.Header.Get("X-Goma-Error"), string(b), err)
	}
	var r io.Reader = response.Body
	switch response.Header.Get("Content-Encoding") {
	case "identity", "":

	case "gzip":
		var err error
		r, err = gzip.NewReader(response.Body)
		if err != nil {
			return err
		}
	case "deflate":
		// RFC7230 says deflate coding is "zlib" (RFC1950) containing
		// "deflate" compressed data (RFC1951).
		// but goma client just used "deflate" compressed data
		// for "Content-Encoding: deflate" wrongly.
		r = flate.NewReader(response.Body)
	default:
		return status.Errorf(codes.InvalidArgument, "unknown content-encoding: %s", response.Header.Get("Content-Encoding"))
	}
	respMsg, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	err = proto.Unmarshal(respMsg, resp)
	if err != nil {
		return err
	}
	return nil
}

// Call calls remote services over http.
func (c *Client) Call(ctx context.Context, req proto.Message, resp proto.Message) error {
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/httprpc.Client.Call")
	defer span.End()
	contentEncoding := c.ContentEncoding
	if contentEncoding == "" {
		contentEncoding = "deflate"
	}
	post, err := serializeToHTTPRequest(ctx, c.URL, req, contentEncoding)
	if err != nil {
		return err
	}
	response, err := client.Do(post)
	if err != nil {
		return err
	}
	err = parseFromHTTPResponse(ctx, response, resp)
	if err != nil {
		return err
	}
	return nil
}

// Call calls remote services over http.
func Call(ctx context.Context, client *http.Client, url string, req proto.Message, resp proto.Message) error {
	c := Client{
		Client: client,
		URL:    url,
	}
	return c.Call(ctx, req, resp)
}
