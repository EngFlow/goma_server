// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package httprpc

import (
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/golang/protobuf/proto"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/rpc"
)

var (
	deflateCompressionLevel = flate.BestSpeed
	gzipCompressionLevel    = gzip.BestSpeed
)

const (
	httpHeader = `X-Cloud-Trace-Context`
)

type encodingType int

const (
	noEncoding encodingType = iota
	encodingDeflate
	encodingGzip
	unknownEncoding
)

func (e encodingType) String() string {
	switch e {
	case noEncoding:
		return "identity"
	case encodingDeflate:
		return "deflate"
	case encodingGzip:
		return "gzip"
	default:
		return fmt.Sprintf("unknownEncoding[%d]", e)
	}
}

func encodingFromHeader(header string) encodingType {
	switch {
	case strings.Contains(header, "gzip"):
		return encodingGzip
	case strings.Contains(header, "deflate"):
		return encodingDeflate
	case header == "", strings.Contains(header, "identity"):
		return noEncoding
	default:
		return unknownEncoding
	}
}

func parseFromHTTPServerRequest(ctx context.Context, req *http.Request, msg proto.Message) (int, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/httprpc.parseFromHTTPServerRequest")
	defer span.End()
	contentEncoding := encodingFromHeader(req.Header.Get("Content-Encoding"))
	var r io.Reader
	switch contentEncoding {
	case noEncoding:
		r = req.Body
	case encodingDeflate:
		// RFC7230 says deflate coding is "zlib" (RFC1950) containing
		// "deflate" compressed data (RFC1951).
		// but goma client just used "deflate" compressed data
		// for "Content-Encoding: deflate" wrongly.
		r = flate.NewReader(req.Body)
	case encodingGzip:
		var err error
		r, err = gzip.NewReader(req.Body)
		if err != nil {
			return 0, status.Errorf(codes.InvalidArgument, "gzip %v", err)
		}
	case unknownEncoding:
		return 0, status.Errorf(codes.InvalidArgument, "unknown encoding: %s", req.Header.Get("Content-Encoding"))
	}
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return 0, err
	}
	return len(data), proto.Unmarshal(data, msg)
}

// serializeToResponseWriter serialize msg to w.
// it returns raw message size, so might differ to actual size if compressed.
func serializeToResponseWriter(ctx context.Context, w http.ResponseWriter, msg proto.Message, acceptEncoding encodingType) (n int, err error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/httprpc.serializeToResponseWriter")
	defer span.End()
	w.Header().Set("Content-Type", "binary/x-protocol-buffer")
	// Accept-Encoding: deflate only if client didn't say gzip,
	// since old goma client only recognizes "Accept-Encoding: deflate".
	// TODO: always accept gzip, deflate once new goma client released.
	if acceptEncoding == encodingGzip {
		w.Header().Set("Accept-Encoding", "gzip, deflate")
	} else {
		w.Header().Set("Accept-Encoding", "deflate")
	}

	resp, err := proto.Marshal(msg)
	if err != nil {
		return 0, err
	}
	var wr io.Writer
	wr = w
	if len(resp) > 0 {
		switch acceptEncoding {
		case noEncoding, unknownEncoding:
			wr = w
			w.Header().Set("Content-Encoding", "identity")
		case encodingDeflate:
			wr, err = flate.NewWriter(w, deflateCompressionLevel)
			if err != nil {
				return 0, err
			}
			defer func() {
				ferr := wr.(*flate.Writer).Close()
				if err == nil {
					err = ferr
				}
			}()
			w.Header().Set("Content-Encoding", "deflate")
		case encodingGzip:
			wr, err = gzip.NewWriterLevel(w, gzipCompressionLevel)
			if err != nil {
				return 0, err
			}
			defer func() {
				ferr := wr.(*gzip.Writer).Close()
				if err == nil {
					err = ferr
				}
			}()
			w.Header().Set("Content-Encoding", "gzip")
		}
	}
	return wr.Write(resp)
}

// RemoteAddr returns http's remote (client) addr.
// https://cloud.google.com/compute/docs/load-balancing/http/#components
func RemoteAddr(req *http.Request) string {
	forwards := req.Header.Get("X-Forwarded-For")
	// initial IP in the comma-separated list.
	s := strings.Split(forwards, ",")
	ip := strings.TrimSpace(s[0])
	if ip == "" {
		// "<ip>:<port>"
		return req.RemoteAddr
	}
	return ip
}

type option struct {
	timeout   time.Duration
	retry     rpc.Retry
	apiKey    string
	cluster   string
	namespace string
	Auth      Auth
}

// HandlerOption sets option for handler.
type HandlerOption func(*option)

// Timeout sets timeout to the handler. Default is 1 second.
func Timeout(d time.Duration) HandlerOption {
	return func(o *option) {
		o.timeout = d
	}
}

// WithRetry sets retry config to the handler.
func WithRetry(retry rpc.Retry) HandlerOption {
	return func(o *option) {
		o.retry = retry
	}
}

// WithAPIKey sets api key in outgoing context.
func WithAPIKey(apiKey string) HandlerOption {
	return func(o *option) {
		o.apiKey = apiKey
	}
}

// WithCluster sets cluster name to the handler for logging/monitoring etc.
func WithCluster(c string) HandlerOption {
	return func(o *option) {
		o.cluster = c
	}
}

// WithNamespace sets cluster namespace to the handler for logging/monitoring etc.
func WithNamespace(ns string) HandlerOption {
	return func(o *option) {
		o.namespace = ns
	}
}

// Auth authenticates the request.
type Auth interface {
	Auth(context.Context, *http.Request) (context.Context, error)
}

// WithAuth sets auth to the handler.
func WithAuth(a Auth) HandlerOption {
	return func(o *option) {
		o.Auth = a
	}
}

func httpStatus(err error) (int, string) {
	// go/http-canonical-mapping
	hc := http.StatusInternalServerError
	var msg string
	switch grpc.Code(err) {
	case codes.OK:
		hc = http.StatusOK

	case codes.Canceled:
		hc = 499
		msg = "Client closed request"

	case codes.InvalidArgument:
		hc = http.StatusBadRequest

	case codes.DeadlineExceeded:
		hc = http.StatusGatewayTimeout

	case codes.NotFound:
		hc = http.StatusNotFound

	case codes.AlreadyExists:
		hc = http.StatusConflict

	case codes.PermissionDenied:
		hc = http.StatusForbidden

	case codes.Unauthenticated:
		// canonical mapping is StatusUnauthrozied (401).
		// however, goma client considers StatusUnauthorized (401)
		// as fatal errors and hard to recover http from
		// the error.
		// we already checked incoming end user credential
		// in Auth, so the request was properly authorized.
		// Unauthenticated would be reported by backend when
		// access token becomes invalid (expired etc).
		// it would happen if access token returned by
		// auth_server, or passed through, has short expiration
		// than request duration. i.e. during handling Exec request,
		// access token is expired.
		// we retries this case, this error should not be returned
		// to user.
		// http://b/78610039 http://b/78662481 http://b/119593170
		hc = http.StatusInternalServerError

	case codes.ResourceExhausted:
		hc = http.StatusTooManyRequests

	case codes.FailedPrecondition:
		hc = http.StatusBadRequest

	case codes.Aborted:
		hc = http.StatusConflict

	case codes.OutOfRange:
		hc = http.StatusBadRequest

	case codes.Unimplemented:
		hc = http.StatusNotImplemented

	case codes.Unavailable:
		hc = http.StatusServiceUnavailable
	}
	if msg == "" {
		msg = http.StatusText(hc)
	}
	return hc, msg
}

// Handler returns http.Handler to serve http rpc handler.
func Handler(name string, req, resp proto.Message, h func(context.Context, proto.Message) (proto.Message, error), opts ...HandlerOption) http.Handler {
	opt := &option{
		timeout: 1 * time.Minute,
	}
	for _, o := range opts {
		o(opt)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), opt.timeout)
		defer cancel()

		if opt.apiKey != "" {
			// https://cloud.google.com/endpoints/docs/grpc/restricting-api-access-with-api-keys-grpc#grpc_clients
			ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", opt.apiKey)
		}

		ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/httprpc.Handler:"+r.URL.Path)
		defer span.End()
		span.AddAttributes(
			trace.StringAttribute("clueter", opt.cluster),
			trace.StringAttribute("namespace", opt.namespace),
		)
		logger := log.FromContext(ctx)

		req := proto.Clone(req)

		// appengine/rp sets Accept-Encoding: gzip?
		acceptEncoding := encodingFromHeader(r.Header.Get("Accept-Encoding"))
		_, err := parseFromHTTPServerRequest(ctx, r, req)
		if err != nil {
			code := http.StatusBadRequest
			http.Error(w, "bad request", code)
			logger.Errorf("incoming parse error %s: %d %s: %v", r.URL.Path, code, http.StatusText(code), err)
			return
		}

		// use short timeout at first, then longer timeout when
		// deadline exceeded, to mitigate grpc lost response case.
		// http://b/129647209
		// assume most call needs less than 1 minute (both exec,
		// file access),
		// according to API metrics, 99%ile of Execute API latency
		// was 33.225 seconds (as of May 15, 2019).
		// only a few exec call may need longer timeout.
		// see below if status.Code(err) == codes.DeadlineExceeded case.
		timeouts := []time.Duration{40 * time.Second, 1 * time.Minute, 3 * time.Minute, 5 * time.Minute}
		var resp proto.Message
		authOK := false
		err = opt.retry.Do(ctx, func() error {
			pctx := ctx
			ctx, cancel := context.WithTimeout(ctx, timeouts[0])
			defer cancel()
			// TODO: hard fail if opt.Auth == nil?
			if opt.Auth != nil {
				ctx, err = opt.Auth.Auth(ctx, r)
				if err != nil {
					if authOK {
						// if once auth ok, then it failed because client access token was expired.
						code := http.StatusGatewayTimeout
						http.Error(w, fmt.Sprintf("auth token expired %s", RemoteAddr(r)), code)
						logger.Errorf("auth token expired %s: %d %s", r.URL.Path, code, http.StatusText(code))
						return err
					}
					code := http.StatusUnauthorized
					http.Error(w, fmt.Sprintf("auth failed %s: %v", RemoteAddr(r), err), code)
					logger.Errorf("auth error %s: %d %s: %v", r.URL.Path, code, http.StatusText(code), err)
					return err
				}
				authOK = true
			}
			resp, err = h(ctx, req)
			if opt.Auth != nil && status.Code(err) == codes.Unauthenticated {
				logger.Warnf("retry for unauthenticated %v", err)
				return rpc.RetriableError{
					Err: err,
				}
			}
			if status.Code(err) == codes.DeadlineExceeded && pctx.Err() == nil {
				// api call is timed out, but caller's context is not.
				// it would happen
				// a) timeout was short; api call actually needs more time.
				// b) api has been finished, but grpc lost response. http://b/129647209
				// Retry with longer timeout.
				// if a, expect to succeed for long run with longer timeout.
				// if b, expect response soon (cache hit).
				if len(timeouts) > 1 {
					timeouts = timeouts[1:]
				}
				logger.Warnf("retry with longer timeout %s", timeouts[0])
				return rpc.RetriableError{
					Err: err,
				}
			}
			return err
		})
		if err != nil {
			span.SetStatus(trace.Status{
				Code:    int32(grpc.Code(err)),
				Message: err.Error(),
			})
			code, msg := httpStatus(err)
			http.Error(w, msg, code)
			switch code {
			case 499: // client closed request
				logger.Warnf("server error %s: %d %s: %v", r.URL.Path, code, msg, err)
			default:
				logger.Errorf("server error %s: %d %s: %v", r.URL.Path, code, msg, err)
			}
			return
		}

		n, err := serializeToResponseWriter(ctx, w, resp, acceptEncoding)

		if err != nil {
			logger.Errorf("outgoing serialize error %s: %v", r.URL.Path, err)
			return
		}
		logger.Debugf("server ok %s: %d", r.URL.Path, n)
	})

	return handler
}

// Handler returns http.Handler to serve http stream.
func StreamHandler(name string, h func(ctx context.Context, w http.ResponseWriter, req *http.Request) error, opts ...HandlerOption) http.Handler {
	opt := &option{
		timeout: 1 * time.Minute,
	}
	for _, o := range opts {
		o(opt)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), opt.timeout)
		defer cancel()

		ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/httprpc.StreamHandler:"+name)
		defer span.End()

		err := h(ctx, w, r)
		if err != nil {
			span.SetStatus(trace.Status{
				Code:    int32(grpc.Code(err)),
				Message: err.Error(),
			})
			code, msg := httpStatus(err)
			http.Error(w, msg, code)
			logger := log.FromContext(ctx)
			logger.Errorf("server error %s: %d %s: %v", r.URL.Path, code, msg, err)
			return
		}
	})
	return handler
}
