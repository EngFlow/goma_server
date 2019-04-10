// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package errorreporter provides error reporting functionality.
package errorreporter

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	"cloud.google.com/go/errorreporting"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"go.chromium.org/goma/server/log"
)

// ErrorReporter is an interface to report crash to stackdriver error reporting.
type ErrorReporter interface {
	Close() error
	Flush()
	Report(e errorreporting.Entry)
	ReportSync(ctx context.Context, e errorreporting.Entry) error
}

var DefaultErrorReporter ErrorReporter = nopErrorReporter{}

// Enabled reports DefaultErrorReporter is configured to report crash to stackdriver.
func Enabled() bool {
	return !reflect.DeepEqual(DefaultErrorReporter, nopErrorReporter{})
}

// New creates error reporter.
func New(ctx context.Context, projectID, serviceName string) ErrorReporter {
	logger := log.FromContext(ctx)
	cfg := errorreporting.Config{
		ServiceName: serviceName,
	}
	ec, err := errorreporting.NewClient(ctx, projectID, cfg)
	if err != nil {
		logger.Warnf("failed to create errorreporting client: %v", err)
		return nopErrorReporter{}
	}
	return ec
}

// Do will be used as defer func to recover panic and report error if
// panic detected. Also update err if err != nil.
func Do(req *http.Request, err *error) {
	if r := recover(); r != nil {
		Report(errorreporting.Entry{
			Error: fmt.Errorf("panic detected: %v", r),
			Req:   req,
		})
		Flush()
		if err != nil {
			*err = grpc.Errorf(codes.Internal, "panic detected: %v", r)
		}
	}
}

// Flush flushes DefaultErrorReporter.
func Flush() {
	DefaultErrorReporter.Flush()
}

// Report reports entry with DefaultErrorReporter.
func Report(e errorreporting.Entry) {
	DefaultErrorReporter.Report(e)
}

// ReportSync reports entry with DefaultErrorReporter.
func ReportSync(ctx context.Context, e errorreporting.Entry) error {
	return DefaultErrorReporter.ReportSync(ctx, e)
}

type nopErrorReporter struct{}

func (e nopErrorReporter) Close() error { return nil }
func (e nopErrorReporter) Flush()       {}

func (e nopErrorReporter) Report(errorreporting.Entry) {}

func (e nopErrorReporter) ReportSync(context.Context, errorreporting.Entry) error { return nil }
