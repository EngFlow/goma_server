// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package log

import (
	"context"
	"log"
	"os"
	"strconv"

	gce "cloud.google.com/go/compute/metadata"
	grpczap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

var (
	gRPCLevel        = zapcore.ErrorLevel
	gRPCVerboseLevel int
)

func mustGRPCLogger() *zap.Logger {
	// emulate grpclog/loggerv2.go
	switch os.Getenv("GRPC_GO_LOG_SEVERITY_LEVEL") {
	case "WARNING", "warning":
		gRPCLevel = zapcore.WarnLevel
	case "INFO", "info":
		gRPCLevel = zapcore.InfoLevel
	default:
		gRPCLevel = zapcore.ErrorLevel
	}

	vLevel := os.Getenv("GRPC_GO_LOG_VERBOSITY_LEVEL")
	if vl, err := strconv.Atoi(vLevel); err == nil {
		gRPCVerboseLevel = vl
	}
	FromContext(context.Background()).Infof("grpc log level = %s verbosity = %d", gRPCLevel, gRPCVerboseLevel)

	zapCfg := zapConfig()
	zapCfg.Level = zap.NewAtomicLevelAt(gRPCLevel)
	gl, err := zapCfg.Build()
	if err != nil {
		FromContext(context.Background()).Fatalf("failed to build grpc zap logger: %v", err)
	}
	// skip 2
	//   zapGRPCLogger method
	//   grpclog func
	// ReplaceGrpcLoggerV2WithVerbosity sets
	// "system=grpc" and "grpc_log=true".
	return gl.WithOptions(zap.AddCallerSkip(2))
}

func init() {
	grpczap.ReplaceGrpcLoggerV2WithVerbosity(grpcLogger, gRPCVerboseLevel)
}

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
	return grpczap.UnaryServerInterceptor(grpcLogger, opts...)
}

func zapConfig() zap.Config {
	if !gce.OnGCE() {
		zapCfg := zap.NewDevelopmentConfig()
		zapCfg.DisableStacktrace = true
		return zapCfg
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
	return zapCfg
}

// mustZapLoggerConfig returns
// * zap logger configured for GKE container if running on compute engine
// * otherwise, use zap's default logger for development outputting non-json text format log.
func mustZapLogger(options ...zap.Option) *zap.Logger {
	logger, err := zapConfig().Build(options...)
	if err != nil {
		log.Fatalf("failed to build zap logger: %v", err)
	}
	return logger
}
