// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Package fswatch watches directory with fsnotify.

*/
package fswatch

import (
	"context"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a directory.
type Watcher struct {
	w      *fsnotify.Watcher
	ch     chan resp
	cancel func()
	errch  chan error
}

type resp struct {
	Event fsnotify.Event
	Err   error
}

// New creates new watcher watching directory.
// It will close when ctx is cancelled.
func New(ctx context.Context, dir string) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	ch := make(chan resp, 10)
	errch := make(chan error)
	ctx, cancel := context.WithCancel(ctx)
	watcher := &Watcher{
		w:      w,
		ch:     ch,
		cancel: cancel,
		errch:  errch,
	}
	go watcher.loop(ctx)
	err = w.Add(dir)
	if err != nil {
		watcher.Close()
		return nil, err
	}
	return watcher, nil
}

// Close stops watcher.
func (w *Watcher) Close() error {
	w.cancel()
	return <-w.errch
}

func (w *Watcher) loop(ctx context.Context) {
	defer func() {
		w.errch <- w.w.Close()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-w.w.Events:
			w.ch <- resp{Event: event}
		case err := <-w.w.Errors:
			w.ch <- resp{Err: err}
		}
	}
}

// Next gets next watched event.
func (w *Watcher) Next(ctx context.Context) (fsnotify.Event, error) {
	select {
	case <-ctx.Done():
		return fsnotify.Event{}, ctx.Err()

	case resp := <-w.ch:
		return resp.Event, resp.Err
	}
}
