// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package remoteapis comes from https://github.com/bazelbuild/remote-apis.
// commit cfe8e540cbb424e3ebc649ddcbc91190f70e23a6
package remoteapis

//go:generate protoc -I.. -I. --go_out=plugins=grpc,Mgoogle/protobuf/timestamp.proto=github.com/golang/protobuf/ptypes/timestamp,Mgoogle/protobuf/duration.proto=github.com/golang/protobuf/ptypes/duration,Mgoogle/longrunning/operations.proto=google.golang.org/genproto/googleapis/longrunning,Mgoogle/rpc/status.proto=google.golang.org/genproto/googleapis/rpc/status,Mbuild/bazel/semver/semver.proto=go.chromium.org/goma/server/proto/remote-apis/build/bazel/semver:. build/bazel/remote/execution/v2/remote_execution.proto
//go:generate protoc -I. --go_out=plugins=grpc:. build/bazel/semver/semver.proto
