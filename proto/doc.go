// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package proto is top directory for proto packages.
//
// Standard goma APIs
//
//  api: package api defines data used on goma APIs.
//  exec: package exec defines exec_service (/cxx-compiler-service/{e,me})
//  file: package file defines file_service (/cxx-compiler-service/{l,s})
//  execlog: package execlog defines log_service (/cxx-compiler-service/sl)
//
// New (internal) APIs
//
//  cache: package cache defines cache_service, backend of exec_service,
//            file_service etc.
//
//  command: package command defines data and service to run command in
//    isolated environment.
//
package proto

//go:generate ./gen_protoc-gen-go
//go:generate ./copy_google_protobuf.sh
//go:generate protoc -I. --go_out=plugins=grpc:. api/goma_data.proto api/goma_log.proto
//go:generate protoc -I. --go_out=plugins=grpc,Mapi/goma_data.proto=go.chromium.org/goma/server/proto/api:. exec/exec_service.proto
//go:generate protoc -I. --go_out=plugins=grpc,Mapi/goma_data.proto=go.chromium.org/goma/server/proto/api:. file/file_service.proto
//go:generate protoc -I. --go_out=plugins=grpc,Mapi/goma_log.proto=go.chromium.org/goma/server/proto/api:. execlog/log_service.proto

//go:generate protoc -I. --go_out=plugins=grpc:. cache/cache.proto cache/cache_service.proto

//go:generate protoc -I. --go_out=plugins=grpc,Mapi/goma_data.proto=go.chromium.org/goma/server/proto/api,Mgoogle/protobuf/timestamp.proto=github.com/golang/protobuf/ptypes/timestamp:. command/command.proto command/command_service.proto command/setup.proto command/package_opts.proto

//go:generate protoc -I. --go_out=plugins=grpc,Mgoogle/protobuf/timestamp.proto=github.com/golang/protobuf/ptypes/timestamp:. auth/auth.proto auth/acl.proto auth/auth_service.proto auth/authdb.proto auth/authdb_service.proto

//go:generate protoc -I. --go_out=plugins=grpc:. backend/backend.proto

//go:generate protoc -I. --go_out=plugins=grpc:. settings/settings.proto settings/settings_service.proto
