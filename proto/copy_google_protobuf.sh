#!/bin/sh
# Copyright 2019 Google Inc. All Rights Reserved.
protobufdir="$(go list -m -f '{{.Dir}}' github.com/golang/protobuf)"
cp "${protobufdir}/ptypes/timestamp/timestamp.proto" \
  ./google/protobuf/timestamp.proto
