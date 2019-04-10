# Goma server

*Goma* is a distributed compiler service for open-source project such as
Chromium and Android. It's some kind of replacement of distcc+ccache.

This is reference implementation of server code to be used with
[Goma client](https://chromium.googlesource.com/infra/goma/client).

[TOC]

## Dependencies

The Goma server uses a backend service that implements the [Remote Execution
API](https://github.com/bazelbuild/remote-apis)
to distribute compile requests across a collection of worker machines and to
cache the results of compilations. The Remote Execution API is an open-source
standard, with multiple service implementations. The Goma server has been tested
with Google's Remote Build Execution service, but could use other service
implementations with some minor tweaks to the service code.

To learn more about Remote Build Execution, which is currently an alpha service,
you can view the whitelisted documentation by joining
[this group](https://groups.google.com/forum/#!forum/rbe-alpha-customers). 
Remote Build Execution instructions are coming soon.

## How to build

Goma server can be built on Linux.

```
$ go get go.chromium.org/goma/server/cmd/remoteexec_proxy
```

You will get the binary in `$(go env GOPATH)/bin`.

## How to run

`remoteexec_proxy` is a single server that acts as proxy server
between Goma client and Remote Execution API.

```
$ remoteexec_proxy --port $PORT \
   --platform-container-image "docker://...@sha256:..." \
   --remoteexec-addr $REMOTEEXEC_ADDR \
   --remote-instance-name $REMOTE_INSTANCE_NAME
```

for chromium, platform container image should be created with
[install-build-dep.sh](https://chromium.googlesource.com/chromium/src/+/master/build/install-build-deps.sh).

Example Dockerfile
```
FROM ubuntu:14.04
ENV DEBIAN_FRONTEND noninteractive

ADD https://chromium.googlesource.com/chromium/src/+/master/build/install-build-deps.sh?format=TEXT /tmp/install-build-deps.sh.base64
RUN base64 -d /tmp/install-build-deps.sh.base64 > /tmp/install-build-deps.sh \
  && \
  apt-get -y update \
  && \
  apt-get install -f -y build-essential gcc-4.8 g++-4.8 docker.io \
  && \
  bash /tmp/install-build-deps.sh --lib32 --no-prompt \
     --no-chromeos-fonts \
  && \
  rm -rf /var/lib/apt/lists/*

```

If Remote Execution API requires service account,
specify service account JSON file for Remote Execution API by
`--service-account-json`.

Running user is granted by default. If you need to allow other users, you
need to specify them by `--whitelisted-users`.

Log messages will be output to stderr.
[OpenCensus z-pages](https://opencensus.io/zpages/go/) ara available at http://localhost:$PORT/debug/

### How to use

Install goma client. We provide prebuilt binary with `cipd`, which
is available in [`depot_tools`](https://commondatastorage.googleapis.com/chrome-infra-docs/flat/depot_tools/docs/html/depot_tools.html).

```
$ cipd install infra/goma/client/linux-amd64 -root ${HOME}/goma
```

or follow the
[build instructions](https://chromium.googlesource.com/infra/goma/client#build) to
build your own local version of Goma client before running the server code,
and install it in `$HOME/goma`.

Need to authenticate Goma client before use.

```
$ $HOME/goma/goma_auth.py login
```

Specify hostname in `$GOMA_SERVER_HOST`
and port in `$GOMA_SERVER_PORT`, along with a few other environment flags.

```
$ export GOMA_SERVER_HOST='host-of-remoteexec_proxy-running'
$ export GOMA_SERVER_PORT='port-of-remoteexec_proxy-running'
$ export GOMA_USE_SSL=false
$ export GOMA_ARBITRARY_TOOLCHAIN_SUPPORT=true
```

For example, if you are running remoteexec_proxy locally with `--port 5050`, use:

```
$ export GOMA_SERVER_HOST=localhost
$ export GOMA_SERVER_PORT=5050
```

Finally, start Goma client:

```
$ $HOME/goma/goma_ctl.py ensure_start
```

and in chromium tree.
```
$ rm -f out/Release/obj/base/base/base64.o
$ GOMA_USE_LOCAL=false autoninja -C out/Release obj/base/base/base64.o
```
