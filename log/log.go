// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Package log provides logging mechanism for goma servers.

*/
package log

import (
	"context"
	"fmt"
	"sync"

	"cloud.google.com/go/compute/metadata"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logger = mustZapLogger()

	tagKeys []tag.Key

	projectIDonce sync.Once
	projectID     string
)

// SetZapLogger sets zap logger as default logger.
// Useful for test
//   log.SetZapLogger(zap.NewExample())
func SetZapLogger(zapLogger *zap.Logger) {
	logger = zapLogger
	setGRPCLogger()
}

// RegsiterTagKey registers tag key that would be used for log context.
func RegisterTagKey(key tag.Key) {
	tagKeys = append(tagKeys, key)
}

// FromContext returns logger with context.
// opencensus's tag registered by RegisterTagKey and
// trace's span-id and trace-id will be added
// as context information of the log.
func FromContext(ctx context.Context) Logger {
	tm := tag.FromContext(ctx)

	var fields []zapcore.Field

	for _, tk := range tagKeys {
		v, ok := tm.Value(tk)
		if ok {
			fields = append(fields, zap.String(tk.Name(), v))
		}
	}
	span := trace.FromContext(ctx)
	var projErr error
	if span.IsRecordingEvents() {
		sc := span.SpanContext()
		traceID := sc.TraceID.String()
		projectIDonce.Do(func() {
			projectID, projErr = metadata.ProjectID()
		})
		if projectID != "" {
			traceID = fmt.Sprintf("projects/%s/traces/%s", projectID, sc.TraceID.String())
		}

		fields = append(fields,
			zap.String("logging.googleapis.com/trace", traceID),
			zap.String("logging.googleapis.com/spanId", sc.SpanID.String()))
	}
	l := logger.With(fields...).Sugar()
	if projErr != nil {
		l.Errorf("metadata.ProjectID: %v", projErr)
	}
	return l
}

// Logger is logging interface.
type Logger interface {
	// Debug logs to DEBUG log. Arguments are handled in the manner of fmt.Print.
	Debug(args ...interface{})

	// Debugf logs to DEBUG log. Arguments are handled in the manner of fmt.Printf.
	Debugf(format string, arg ...interface{})

	// Info logs to INFO log. Arguments are handled in the manner of fmt.Print.
	Info(args ...interface{})
	// Infof logs to INFO log. Arguments are handled in the manner of fmt.Printf.
	Infof(format string, arg ...interface{})

	// Warn logs to WARNING log. Arguments are handled in the manner of fmt.Print.
	Warn(args ...interface{})

	// Warnf logs to WARNING log. Arguments are handled in the manner of fmt.Printf.
	Warnf(format string, arg ...interface{})

	// Error logs to ERROR log. Arguments are handled in the manner of fmt.Print.
	Error(args ...interface{})

	// Errorf logs to ERROR log. Arguments are handled in the manner of fmt.Printf.
	Errorf(format string, arg ...interface{})

	// Fatal logs to CRITICAL log. Arguments are handled in te manner of fmt.Print.
	Fatal(args ...interface{})

	// Fatalf logs to CRITICAL log. Arguments are handled in the manner of fmt.Printf.
	Fatalf(format string, arg ...interface{})

	// Sync flushes any buffered log entries.
	Sync() error
}
