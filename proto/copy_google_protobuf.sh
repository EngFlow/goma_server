#!/bin/sh -e
# Copyright 2019 Google LLC. All Rights Reserved.
# well-known protobufs are shipped with protoc.
#
# /usr/local/bin/protoc -> /usr/local/include/google/protobuf/*
# cipd protoc -> .cipd_bin/include/google/protobuf/*
# libprotobuf-dev -> /usr/include/google/protobuf/*

protocpath="$(which protoc)"
case "${protocpath}" in
/usr/local/bin/protoc)
  # we install protoc in /usr/local in our docker image.
  incdir=/usr/local/include/google/protobuf
  ;;

/usr/bin/protoc)
  # protobuf-compiler + libprotobuf-dev
  if [ ! -f /usr/include/google/protobuf/timestamp.proto ]; then
    echo 'need to install libprotobuf-dev' >&2
    exit 1
  fi
  incdir=/usr/include/google/protobuf
  ;;

*)
  # cipd package (used in presubmit builder)
  incdir="$(dirname "${protocpath}")/.cipd_bin/include/google/protobuf"
  ;;
esac
cp "${incdir}/timestamp.proto" ./google/protobuf/timestamp.proto
