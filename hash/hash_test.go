// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package hash

import (
	"io/ioutil"
	"os"
	"testing"

	pb "go.chromium.org/goma/server/proto/api"
)

func TestSHA256HMAC(t *testing.T) {
	// https://tools.ietf.org/html/rfc4231
	key := []byte("Jefe")
	data := []byte("what do ya want for nothing?")
	expected := "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
	actual := SHA256HMAC(key, data)
	if actual != expected {
		t.Errorf("SHA256HMAC(%s, %s)=%s; want %s", key, data, actual, expected)
	}
}

func TestSHA256Content(t *testing.T) {
	b := []byte{}
	hk := SHA256Content(b)
	expectedHk := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if hk != expectedHk {
		t.Errorf("SHA256Content(%q) = %q; want %q", b, hk, expectedHk)
	}
	b2 := byte('\n')
	b3 := append([]byte(hk), b2)
	hk2 := SHA256Content(b3)
	expectedHk2 := "38acb15d02d5ac0f2a2789602e9df950c380d2799b4bdb59394e4eeabdd3a662"
	if hk2 != expectedHk2 {
		t.Errorf("SHA256Content(%q) = %q; want %q", b3, hk2, expectedHk2)
	}
}

func TestSHA256Proto(t *testing.T) {
	fb := &pb.FileBlob{
		BlobType: pb.FileBlob_FILE.Enum(),
	}
	actual, err := SHA256Proto(fb)
	if err != nil {
		t.Errorf("SHA256Proto(%q) error returns %v; want nil", fb, err)
	}
	expected := "fb8da7eb5b1b399e7321179dac9e9f65773d7331e1e30554e3911e4325e1ef19"
	if actual != expected {
		t.Errorf("SHA256Proto(%q) return %v; want %v", fb, actual, expected)
	}
}

func TestSHA256ProtoWithMarshalFailure(t *testing.T) {
	_, err := SHA256Proto(nil)
	if err == nil {
		t.Errorf("SHA256Proto(nil) error is nil; want error")
	}
}

func TestSHA256File(t *testing.T) {
	tempDir, err := ioutil.TempDir(os.TempDir(), "sha256dir")
	if err != nil {
		t.Fatalf("couldn't make a temporary directory")
	}
	defer os.RemoveAll(tempDir)

	tempFile, err := ioutil.TempFile(tempDir, "test")
	if err != nil {
		t.Fatalf("couldn't make a temporary file")
	}

	filename := tempFile.Name()

	if err := tempFile.Close(); err != nil {
		t.Fatalf("couldn't close a temporary file")
	}

	actual, err := SHA256File(filename)
	if err != nil {
		t.Errorf("SHA256File(%q) error returns %v; want nil", filename, err)
	}

	expected := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if actual != expected {
		t.Errorf("SHA256File(%q) return %v; want %v", filename, actual, expected)
	}
}

func TestSHA256FileWhenFileNotExists(t *testing.T) {
	tempDir, err := ioutil.TempDir(os.TempDir(), "sha256dir")
	if err != nil {
		t.Fatalf("couldn't make a temporary directory")
	}
	defer os.RemoveAll(tempDir)

	filename := "not-exist-file.txt"

	_, err = SHA256File(filename)
	if err == nil {
		t.Errorf("SHA256File(%q) error is nil; want error", filename)
	}
	if !os.IsNotExist(err) {
		t.Errorf("SHA256File(%q) error is %v; want ErrNotExist", filename, err)
	}
}
