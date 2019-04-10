// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package fswatch

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestNext(t *testing.T) {
	dir, err := ioutil.TempDir("", "fswatch.TestNext.")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	ctx := context.Background()
	fmt.Println("new")
	w, err := New(ctx, dir)
	if err != nil {
		t.Fatalf("New(ctx, dir)=_, %v; want nil-err", err)
	}
	defer w.Close()
	timeout := 100 * time.Millisecond

	{
		t.Logf("no event in %s", timeout)
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		_, err := w.Next(ctx)
		if err != context.DeadlineExceeded {
			t.Fatalf("w.Next(ctx)=_, %v; want DeadlineExceeded", err)
		}
	}
	fname := filepath.Join(dir, "foo")
	{
		t.Logf("add new file")
		err = ioutil.WriteFile(fname, []byte("1"), 0644)
		if err != nil {
			t.Fatalf("WriteFile(%q)=%v; want=nil error", fname, err)
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		ev, err := w.Next(ctx)
		want := fsnotify.Event{Name: fname, Op: fsnotify.Create}
		if err != nil || !reflect.DeepEqual(ev, want) {
			t.Fatalf("w.Next(ctx)=%v, %v; want=%v, nil", ev, err, want)
		}
		ev, err = w.Next(ctx)
		want = fsnotify.Event{Name: fname, Op: fsnotify.Write}
		if err != nil || !reflect.DeepEqual(ev, want) {
			t.Fatalf("w.Next(ctx)=%v, %v; want=%v, nil", ev, err, want)
		}
		_, err = w.Next(ctx)
		if err != context.DeadlineExceeded {
			t.Fatalf("w.Next(ctx)=_, %v; want DeadlineExceeded", err)
		}
	}
	{
		t.Logf("update file")
		err = ioutil.WriteFile(fname, []byte("2"), 0644)
		if err != nil {
			t.Fatalf("WriteFile(%q)=%v; want=nil error", fname, err)
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		ev, err := w.Next(ctx)
		want := fsnotify.Event{Name: fname, Op: fsnotify.Write}
		if err != nil || !reflect.DeepEqual(ev, want) {
			t.Fatalf("w.Next(ctx)=%v, %v; want=%v, nil", ev, err, want)
		}

		for {
			ev, err = w.Next(ctx)
			if err == context.DeadlineExceeded {
				break
			}
			// may receive several Write.
			t.Logf("w.Next(ctx)=%v, %v", ev, err)
		}
	}
	{
		t.Logf("remove file")
		err = os.Remove(fname)
		if err != nil {
			t.Fatalf("Remove(%q)=%v; want=nil error", fname, err)
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		ev, err := w.Next(ctx)
		want := fsnotify.Event{Name: fname, Op: fsnotify.Remove}
		if err != nil || !reflect.DeepEqual(ev, want) {
			t.Fatalf("w.Next(ctx)=%v, %v; want=%v, nil", ev, err, want)
		}
		_, err = w.Next(ctx)
		if err != context.DeadlineExceeded {
			t.Fatalf("w.Next(ctx)=_, %v; want DeadlineExceeded", err)
		}
	}
}
