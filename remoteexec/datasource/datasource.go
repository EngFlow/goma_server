/* Copyright 2018 Google Inc. All Rights Reserved. */

// Package datasource provides data source from local file, bytes etc.
package datasource

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/golang/protobuf/proto"
)

// Source accesses data source.
type Source interface {
	Open(context.Context) (io.ReadCloser, error)
	String() string
}

type localFile struct {
	fullpath string
}

// LocalFile creates new source for the local file.
func LocalFile(fullpath string) Source {
	return localFile{
		fullpath: fullpath,
	}
}

func (f localFile) String() string {
	return fmt.Sprintf("localfile:%s", f.fullpath)
}

func (f localFile) Open(context.Context) (io.ReadCloser, error) {
	fp, err := os.Open(f.fullpath)
	return fp, err
}

type byteData struct {
	name string
	data []byte
}

// Bytes create new source for bytes.
func Bytes(name string, b []byte) Source {
	return byteData{
		name: name,
		data: b,
	}
}

func (b byteData) String() string {
	return b.name
}

func (b byteData) Open(context.Context) (io.ReadCloser, error) {
	r := bytes.NewReader(b.data)
	return ioutil.NopCloser(r), nil
}

// ReadAll reads all data from source.
func ReadAll(ctx context.Context, src Source) ([]byte, error) {
	f, err := src.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ioutil.ReadAll(f)
}

// ReadProto reads data as proto message.
func ReadProto(ctx context.Context, src Source, m proto.Message) error {
	b, err := ReadAll(ctx, src)
	if err != nil {
		return err
	}
	return proto.Unmarshal(b, m)
}
