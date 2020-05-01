# nsjail config proto

protobuf schema for [nsjail](http://nsjail.com/)
([GitHub](https://github.com/google/nsjail)).
This is used for providing hermetic build environment with
arbitrary toolchain support.

## How to update the file?

1. git clone

```shell
$ git clone https://github.com/google/nsjail.git
```

1. copy config.proto file.

```shell
$ cp nsjail/config.proto .
```

1. Add `option go_package = "go.chromium.org/goma/server/proto/nsjail";`
