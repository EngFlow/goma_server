#!/bin/sh
# Copyright 2019 Google Inc. All Rights Reserved.
protobufdir="$(go list -m -f '{{.Dir}}' github.com/golang/protobuf)"
cp "${protobufdir}/ptypes/timestamp/timestamp.proto" \
  ./google/protobuf/timestamp.proto

# for remote-apis v2

cp "${protobufdir}/ptypes/duration/duration.proto" \
  ./google/protobuf/duration.proto
cp "${protobufdir}/ptypes/any/any.proto" \
  ./google/protobuf/any.proto
cp "${protobufdir}/ptypes/empty/empty.proto" \
  ./google/protobuf/empty.proto
cp "${protobufdir}/protoc-gen-go/descriptor/descriptor.proto" \
  ./google/protobuf/descriptor.proto
