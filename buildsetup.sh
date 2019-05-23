#!/bin/bash
# Copyright 2019 Google Inc. All Rights Reserved.

set -e

sdkdir="$1"
if [ ! -d "${sdkdir}" ]; then
  echo "usage: $0 sdkdir" >&2
  exit 1
fi
mkdir -p "${sdkdir}/go/bin"

# assume host has ca-certificates, cur, git and gcc.
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

# https://repo1.maven.org/maven2/com/google/protobuf/protoc/${protoc}/protoc-${protoc}-linux-x86_64.exe.sha1
protoc=3.7.0
protocsha1=b8f4dea2467de954ac0aa399f2d60ea36c73a5ae

echo "${protocsha1} ${sdkdir}/go/bin/protoc" > /tmp/protoc.sha1
if ! sha1sum --check /tmp/protoc.sha1; then
  curl -o "${sdkdir}/go/bin/protoc" https://repo1.maven.org/maven2/com/google/protobuf/protoc/${protoc}/protoc-${protoc}-linux-x86_64.exe
  sha1sum --check /tmp/protoc.sha1
fi
chmod a+x "${sdkdir}/go/bin/protoc"
rm -f /tmp/protoc.sha1

# TODO: use 'go get golang.org/dl/${go}' ?
go=go1.12.5
gosha256=aea86e3c73495f205929cfebba0d63f1382c8ac59be081b6351681415f4063cf
goarchive="${sdkdir}/${go}.linux-amd64.tar.gz"
echo "${gosha256} ${goarchive}" > "/tmp/${go}.linux-amd64.tar.gz.sha256"
if ! sha256sum --check "/tmp/${go}.linux-amd64.tar.gz.sha256"; then
  curl -o "${goarchive}" "https://storage.googleapis.com/golang/${go}.linux-amd64.tar.gz"
  sha256sum --check "/tmp/${go}.linux-amd64.tar.gz.sha256"
  tar -C "${sdkdir}" -xzf "${goarchive}"
fi
rm -f "/tmp/${go}.linux-amd64.tar.gz.sha256"

echo "I: protoc-${protoc} and ${go} are installed in ${sdkdir}"
echo "I: set ${sdkdir}/go/bin in \$PATH"
