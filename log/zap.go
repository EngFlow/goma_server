// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package log

import (
	"fmt"
	"log"

	gce "cloud.google.com/go/compute/metadata"
	grpczap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
)

func init() {
	setGRPCLogger()
}

func setGRPCLogger() {
	// skip 2
	//   zapGRPCLogger method
	//   grpclog func
	zgl := &zapGRPCLogger{logger.WithOptions(zap.AddCallerSkip(2)).With(zap.String("system", "grpc"), zap.Bool("grpc_log", true))}
	grpclog.SetLoggerV2(zgl)
}

// zapGRPCLogger for logger for grpc.
// It will log at debug level for grpc Info level log message
// because it is too chatty.
type zapGRPCLogger struct {
	logger *zap.Logger
}

func (l *zapGRPCLogger) Info(args ...interface{}) {
	l.logger.Debug(fmt.Sprint(args...))
}

func (l *zapGRPCLogger) Infoln(args ...interface{}) {
	l.logger.Debug(fmt.Sprint(args...))
}

func (l *zapGRPCLogger) Infof(format string, args ...interface{}) {
	l.logger.Debug(fmt.Sprintf(format, args...))
}

func (l *zapGRPCLogger) Warning(args ...interface{}) {
	l.logger.Warn(fmt.Sprint(args...))
}

func (l *zapGRPCLogger) Warningln(args ...interface{}) {
	l.logger.Warn(fmt.Sprint(args...))
}

func (l *zapGRPCLogger) Warningf(format string, args ...interface{}) {
	l.logger.Warn(fmt.Sprintf(format, args...))
}

func (l *zapGRPCLogger) Error(args ...interface{}) {
	l.logger.Error(fmt.Sprint(args...))
}

func (l *zapGRPCLogger) Errorln(args ...interface{}) {
	l.logger.Error(fmt.Sprint(args...))
}

func (l *zapGRPCLogger) Errorf(format string, args ...interface{}) {
	l.logger.Error(fmt.Sprintf(format, args...))
}

func (l *zapGRPCLogger) Fatal(args ...interface{}) {
	l.logger.Fatal(fmt.Sprint(args...))
}

func (l *zapGRPCLogger) Fatalf(format string, args ...interface{}) {
	l.logger.Fatal(fmt.Sprintf(format, args...))
}

func (l *zapGRPCLogger) Fatalln(args ...interface{}) {
	l.logger.Fatal(fmt.Sprint(args...))
}

func (l *zapGRPCLogger) V(level int) bool { return true }

// GRPCUnaryServerInterceptor returns server interceptor to log grpc calls.
func GRPCUnaryServerInterceptor(opts ...grpczap.Option) grpc.UnaryServerInterceptor {
	opts = append([]grpczap.Option{
		grpczap.WithLevels(func(code codes.Code) zapcore.Level {
			// "finished unary call with code OK" is too chatty
			// (for health check etc).
			switch code {
			case codes.OK:
				return zap.DebugLevel
			}
			return grpczap.DefaultCodeToLevel(code)
		}),
	}, opts...)
	return grpczap.UnaryServerInterceptor(logger, opts...)
}

// mustZapLogger returns
// * zap logger configured for GKE container if running on compute engine
// * otherwise, use zap's default logger for development outputting non-json text format log.
func mustZapLogger(options ...zap.Option) *zap.Logger {
	if !gce.OnGCE() {
		zapCfg := zap.NewDevelopmentConfig()
		zapCfg.DisableStacktrace = true
		logger, err := zapCfg.Build(options...)
		if err != nil {
			log.Fatalf("failed to build zap logger: %v", err)
		}
		return logger
	}

	zapCfg := zap.NewProductionConfig()

	zapCfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)

	// To show text content only in cloud logging top level viewer.
	// https://cloud.google.com/logging/docs/view/logs_viewer_v2#expanding
	zapCfg.EncoderConfig.MessageKey = "message"

	// https://github.com/GoogleCloudPlatform/fluent-plugin-google-cloud/blob/cea0afd43287ce35d6aa3e1b29e791943d379df4/lib/fluent/plugin/out_google_cloud.rb#L950
	zapCfg.EncoderConfig.TimeKey = "time"
	zapCfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// https://github.com/GoogleCloudPlatform/fluent-plugin-google-cloud/blob/cea0afd43287ce35d6aa3e1b29e791943d379df4/lib/fluent/plugin/out_google_cloud.rb#L980
	zapCfg.EncoderConfig.LevelKey = "severity"

	logger, err := zapCfg.Build(options...)
	if err != nil {
		log.Fatalf("failed to build zap logger: %v", err)
	}
	return logger
}
