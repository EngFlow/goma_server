/* Copyright 2018 Google Inc. All Rights Reserved. */

package cas

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	rpb "go.chromium.org/goma/server/proto/remote-apis/build/bazel/remote/execution/v2"

	"github.com/google/uuid"
	"go.opencensus.io/trace"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/bytestreamio"
	"go.chromium.org/goma/server/log"
)

// The current maximum data chunk size for both Read() and Write() is 2MB.
const maxChunkSizeBytes = 2 * 1024 * 1024

// ParseResName parses resource name; digest string formatted as "blobs/<hash>/<sizebytes>".
// It ignores prior to "blobs", and after <sizebytes>.
// https://github.com/bazelbuild/remote-apis/blob/c1c1ad2c97ed18943adb55f06657440daa60d833/build/bazel/remote/execution/v2/remote_execution.proto#L168
func ParseResName(name string) (*rpb.Digest, error) {
	pc := strings.Split(name, "/")
	for i, s := range pc {
		if s == "blobs" && i+2 < len(pc) {
			hash := strings.ToLower(pc[i+1])
			if len(hash) != 64 {
				return nil, fmt.Errorf("%q: hash %q is not 64 bytes", name, pc[i+1])
			}
			if hash != pc[i+1] {
				return nil, fmt.Errorf("%q: hash %q is not lowercase", name, pc[i+1])
			}
			_, err := hex.DecodeString(hash)
			if err != nil {
				return nil, fmt.Errorf("%q: hash %q is not hex string: %v", name, pc[i+1], err)
			}
			n, err := strconv.ParseInt(pc[i+2], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%q: sizebytes %q is not number: %v", name, pc[i+2], err)
			}
			return &rpb.Digest{
				Hash:      hash,
				SizeBytes: n,
			}, nil
		}
	}
	return nil, fmt.Errorf("%q: not resource name", name)
}

func UploadDigest(ctx context.Context, bs bpb.ByteStreamClient, instance string, digest *rpb.Digest, rd io.Reader) error {
	resname := UploadResName(instance, digest)
	return Upload(ctx, bs, resname, digest.SizeBytes, rd)
}

// UploadResName returns resource name of digest in instance to upload.
// https://github.com/bazelbuild/remote-apis/blob/c1c1ad2c97ed18943adb55f06657440daa60d833/build/bazel/remote/execution/v2/remote_execution.proto#L187
func UploadResName(instance string, digest *rpb.Digest) string {
	uuid := uuid.New()
	return path.Join(instance, "uploads", uuid.String(), "blobs", digest.Hash, strconv.FormatInt(digest.SizeBytes, 10))
}

// Upload uploads blob specified by resname from rd.
func Upload(ctx context.Context, bs bpb.ByteStreamClient, resname string, size int64, rd io.Reader) error {
	span := trace.FromContext(ctx)
	logger := log.FromContext(ctx)
	logger.Infof("upload %s", resname)
	span.AddAttributes(trace.StringAttribute("resname", resname))

	wr, err := bytestreamio.Create(ctx, bs, resname)
	if err != nil {
		s := status.Convert(err)
		return status.Errorf(s.Code(), "upload write %s: %v", resname, s.Message())
	}
	t0 := time.Now()
	buf := make([]byte, maxChunkSizeBytes)
	written, err := io.CopyBuffer(wr, rd, buf)
	if err != nil {
		wr.Close()
		logger.Warnf("upload failed %s %d in %s: %v", resname, written, time.Since(t0), err)
		s := status.Convert(err)
		return status.Errorf(s.Code(), "upload error %s %d: %v", resname, written, s.Message())
	}
	err = wr.Close()
	if err != nil {
		s := status.Convert(err)
		return status.Errorf(s.Code(), "upload close: %s: %v", resname, s.Message())
	}
	logger.Infof("upload %s in %s", resname, time.Since(t0))
	return nil
}

// DownloadDigest downloads blob specified resname/digest into w.
func DownloadDigest(ctx context.Context, bs bpb.ByteStreamClient, wr io.Writer, instance string, digest *rpb.Digest) error {
	resname := ResName(instance, digest)
	size, err := Download(ctx, bs, wr, resname)
	if err != nil {
		return err
	}
	if size != digest.SizeBytes {
		return fmt.Errorf("incomplete fetch %v: size=%d", digest, size)
	}
	return nil
}

// ResName returns resource name of digest in instance.
// https://github.com/bazelbuild/remote-apis/blob/c1c1ad2c97ed18943adb55f06657440daa60d833/build/bazel/remote/execution/v2/remote_execution.proto#L220
func ResName(instance string, digest *rpb.Digest) string {
	return path.Join(instance, "blobs", digest.Hash, strconv.FormatInt(digest.SizeBytes, 10))
}

// Download downloads blob specified by resname into w.
func Download(ctx context.Context, bs bpb.ByteStreamClient, wr io.Writer, resname string) (int64, error) {
	span := trace.FromContext(ctx)
	logger := log.FromContext(ctx)
	t := time.Now()
	logger.Infof("download %s", resname)
	span.AddAttributes(trace.StringAttribute("resname", resname))

	rd, err := bytestreamio.Open(ctx, bs, resname)
	if err != nil {
		s := status.Convert(err)
		return 0, status.Errorf(s.Code(), "download read: %s: %v", resname, s.Message())
	}
	buf := make([]byte, maxChunkSizeBytes)
	written, err := io.CopyBuffer(wr, rd, buf)
	if err != nil {
		logger.Warnf("download failed %s %d in %s: %v", resname, written, time.Since(t), err)
		s := status.Convert(err)
		return written, status.Errorf(s.Code(), "download error %s %d: %v", resname, written, s.Message())
	}
	logger.Infof("download %s in %s", resname, time.Since(t))
	return int64(written), nil
}
