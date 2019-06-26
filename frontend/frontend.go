// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package frontend

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/plugin/ochttp/propagation/tracecontext"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"

	"go.chromium.org/goma/server/httprpc"
	"go.chromium.org/goma/server/log"
	"go.chromium.org/goma/server/log/errorreporter"
)

const (
	// PathPrefix is goma endpoint prefix.
	PathPrefix = "/cxx-compiler-service/"
)

var (
	hostname string

	pingRequests = stats.Int64(
		"go.chromium.org/goma/server/frontend.ping_count",
		"Number of ping requests",
		stats.UnitDimensionless)

	// commit hash and time specified in user-agent.
	// the value of these tag can be controlled by the client,
	// so you need to watch out for potentially generating high-cardinality
	// labels in your metrics backend if you use this tag in views.
	userAgentCommitHashKey = tag.MustNewKey("useragent.hash")
	userAgentCommitTimeKey = tag.MustNewKey("useragent.time")

	// user-agent format
	//  compiler-proxy built by <username> at <commitHash>@<commitTime>
	//  on <date-time>.
	// see https://chromium.googlesource.com/infra/goma/client/+/70685d6cbb19c108d8abf2235edd2d02bed8dded/client/generate_compiler_proxy_info.py#87
	userAgentRE = regexp.MustCompile(`compiler-proxy built by \S+ at ([[:xdigit:]]+)@([[:digit:]]+) on .*`)

	// DefaultViews are the default views provided by this package.
	// You need to register he view for data to actually be collected.
	DefaultViews = []*view.View{
		{
			Name:        "go.chromium.org/goma/server/frontend.ping_count_by_useragent",
			Description: "ping request count by user-agent",
			TagKeys: []tag.Key{
				userAgentCommitHashKey,
				userAgentCommitTimeKey,
			},
			Measure:     pingRequests,
			Aggregation: view.Count(),
		},
	}
)

func init() {
	var err error
	hostname, err = os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
}

func parseUserAgent(header string) (commitHash, commitTime string, err error) {
	m := userAgentRE.FindStringSubmatch(header)
	if len(m) == 0 {
		return "", "", fmt.Errorf("unexpected client: %s", header)
	}
	return m[1], m[2], nil
}

func checkUserAgent(ctx context.Context, req *http.Request) error {
	var tags []tag.Mutator
	commitHash, commitTime, err := parseUserAgent(req.Header.Get("User-Agent"))
	if err != nil {
		// TODO: reject requests from unexpected client?
		logger := log.FromContext(ctx)
		logger.Errorf("user-agent: %v", err)
		tags = append(tags,
			tag.Upsert(userAgentCommitHashKey, "error"),
			tag.Upsert(userAgentCommitTimeKey, "error"))
	} else {
		// TODO: reject requests from too old client? http://b/110381625
		tags = append(tags,
			tag.Upsert(userAgentCommitHashKey, commitHash),
			tag.Upsert(userAgentCommitTimeKey, commitTime))
	}
	ctx, err = tag.New(ctx, tags...)
	if err != nil {
		return err
	}
	stats.Record(ctx, pingRequests.M(1))
	return nil
}

// Backend represents backend of goma frontend.
type Backend interface {
	Ping() http.Handler
	Exec() http.Handler
	ByteStream() http.Handler
	StoreFile() http.Handler
	LookupFile() http.Handler
	Execlog() http.Handler
}

// Frontend represents goma frontend.
type Frontend struct {
	AC      httprpc.AdmissionController
	Backend Backend

	TraceLabels map[string]string

	// TODO: health status?
	// TODO: downloadurl?
	// TODO: compilers? - drop support?
}

// Handler creates http.Handler from Frontend.
func Handler(f Frontend) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/ping", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/frontend.Handler.ping")
		defer span.End()

		var buf bytes.Buffer
		err := req.Write(&buf)
		span.AddAttributes(trace.StringAttribute("header", buf.String()))
		if err != nil {
			span.AddAttributes(trace.StringAttribute("err", err.Error()))
		}
		err = checkUserAgent(ctx, req)
		if err != nil {
			logger := log.FromContext(ctx)
			logger.Errorf("failed to record user-agent: %v", err)
		}

		f.Backend.Ping().ServeHTTP(w, req)
	}))
	mux.Handle("/e", f.Backend.Exec())
	mux.Handle("/blobs/", f.Backend.ByteStream())
	mux.Handle("/s", f.Backend.StoreFile())
	mux.Handle("/l", f.Backend.LookupFile())
	mux.Handle("/sl", f.Backend.Execlog())
	// TODO: /downloadurl etc?

	h := httprpc.AdmissionControl(f.AC, mux)
	return h
}

type reportingResponseWriter struct {
	http.ResponseWriter
	er  errorreporter.ErrorReporter
	req *http.Request
}

func (w reportingResponseWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
	if statusCode < http.StatusBadRequest || statusCode == 499 {
		// 499 is Canceled. go/http-canonical-mapping
		return
	}
	ctx := w.req.Context()
	logger := log.FromContext(ctx)
	logger.Errorf("http %d %s", statusCode, http.StatusText(statusCode))
}

func (f Frontend) errorReport(h http.Handler) http.Handler {
	if errorreporter.Enabled() {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer errorreporter.Do(req, nil)
		h.ServeHTTP(reportingResponseWriter{
			ResponseWriter: w,
			req:            req,
		}, req)
	})
}

// Register registers Frontend under PathPrefix (/cxx-compiler-service).
func Register(mux *http.ServeMux, f Frontend) {
	h := Handler(f)
	h = http.StripPrefix(PathPrefix[:len(PathPrefix)-1], h)
	h = httprpc.Trace(h, f.TraceLabels)
	h = f.errorReport(h)
	mux.Handle(PathPrefix, &ochttp.Handler{
		Propagation: &tracecontext.HTTPFormat{},
		Handler:     h,
	})
}
