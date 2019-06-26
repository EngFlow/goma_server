#!/bin/bash
# Copyright 2019 Google Inc. All Rights Reserved.

set -e

script_dir=$(cd $(dirname $0); pwd)
sdk_dir="$1"
if [ ! -d "${sdk_dir}" ]; then
  echo "usage: $0 sdk_dir" >&2
  exit 1
fi

# assume host has ca-certificates, curl, git and gcc.
pkg_missing=false
for pkg in ca-certificates curl git gcc; do
  if ! dpkg-query -s "$pkg" >/dev/null; then
    echo "E: $pkg not installed" >&2
    pkg_missing=true
  fi
done
if $pkg_missing; then
  echo "E: package(s) missing" >&2
  exit 1
fi

cipd_dir="${sdk_dir}/.cipd_bin"
cipd_manifest="$script_dir/cipd_manifest.txt"
mkdir -p "$cipd_dir"
cipd ensure -log-level warning \
  -ensure-file "$cipd_manifest"\
  -root "$cipd_dir"

(cd "$sdk_dir"; rm -f go; ln -s .cipd_bin/bin/go)
(cd "$sdk_dir"; rm -f protoc; ln -s .cipd_bin/protoc)

echo "I: protoc and go are installed in ${sdk_dir}"
echo "I: set ${sdk_dir} in \$PATH"
