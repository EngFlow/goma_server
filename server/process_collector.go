// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"

	"go.chromium.org/goma/server/log"
)

var (
	openFDs = stats.Int64("go.chromium.org/goma/server/server/process-open-fds",
		"Number of open file descriptors",
		stats.UnitDimensionless)
	maxFDs = stats.Int64("go.chromium.org/goma/server/server/process-max-fds",
		"Maximum number of open file descriptors",
		stats.UnitDimensionless)

	virtualMemorySize = stats.Int64("go.chromium.org/goma/server/server/process-virtual-memory",
		"Virtual memory size",
		stats.UnitBytes)
	residentMemorySize = stats.Int64("go.chromium.org/goma/server/server/process-resident-memory",
		"Resident memory size",
		stats.UnitBytes)

	procStatViews = []*view.View{
		{
			Name:        "go.chromium.org/goma/server/server/process-open-fds",
			Description: "Number of open file descriptors",
			Measure:     openFDs,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "go.chromium.org/goma/server/server/process-max-fds",
			Description: "Maxinum number of open file descriptors",
			Measure:     maxFDs,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "go.chromium.org/goma/server/server/process-virtual-memroy",
			Description: "Virtual memory size",
			Measure:     virtualMemorySize,
			Aggregation: view.LastValue(),
		},
		{
			Name:        "go.chromium.org/goma/server/server/process-resident-memory",
			Description: "Resident memory size",
			Measure:     residentMemorySize,
			Aggregation: view.LastValue(),
		},
	}

	lastResidentMemorySize int64 // atomic.

	samplingInterval = time.Second
)

// ResidentMemorySize reports latest measured resident memory size in bytes.
func ResidentMemorySize() int64 {
	return atomic.LoadInt64(&lastResidentMemorySize)
}

// GC runs garbage-collector and reports latest measured resident memory size in bytes.
func GC(ctx context.Context) int64 {
	logger := log.FromContext(ctx)
	rss := ResidentMemorySize()
	logger.Infof("GC start: rss=%d", rss)
	runtime.GC()
	procStats(ctx)
	rss = ResidentMemorySize()
	logger.Infof("GC end: rss=%d", rss)
	return rss
}

func numOpenFDs(ctx context.Context) (int64, error) {
	d, err := os.Open("/proc/self/fd")
	if err != nil {
		return 0, err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return 0, err
	}
	return int64(len(names)), nil
}

func parseMaxFDs(ctx context.Context, r io.Reader) (int64, error) {
	// soft limit of "Max open files"
	const maxOpenFiles = `Max open files`
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Bytes()
		if !bytes.HasPrefix(line, []byte(maxOpenFiles)) {
			continue
		}
		line = bytes.TrimPrefix(line, []byte(maxOpenFiles))
		line = bytes.TrimSpace(line)
		cols := bytes.Fields(line)
		if len(cols) == 0 {
			return 0, fmt.Errorf("wrong line for Max open files: %q", string(line))
		}
		return strconv.ParseInt(string(cols[0]), 10, 64)
	}
	err := s.Err()
	if err != nil {
		return 0, err
	}
	return 0, errors.New(`"Max open files" not found`)
}

func numMaxFDs(ctx context.Context) (int64, error) {
	f, err := os.Open("/proc/self/limits")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return parseMaxFDs(ctx, f)
}

func parseStatMemory(ctx context.Context, r io.Reader) (vsize, rss int64, err error) {
	// see proc(5)
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return 0, 0, err
	}
	i := bytes.LastIndex(data, []byte(")"))
	if i < 0 {
		return 0, 0, fmt.Errorf("unexpected format of stat (no comm): %q", string(data))
	}
	cols := bytes.Fields(data[i+2:])
	if len(cols) < 22 {
		return 0, 0, fmt.Errorf("unexpected format of stat (few data): %q %d", string(data), len(cols))
	}
	// cols starts from (3) state.
	// we want (23) vsize and (24) rss.
	const vsizeIndex = 23 - 3
	const rssIndex = 24 - 3
	vsize, err = strconv.ParseInt(string(cols[vsizeIndex]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse vsize %q: %v", string(cols[vsizeIndex]), err)
	}
	rss, err = strconv.ParseInt(string(cols[rssIndex]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse rss %q: %v", string(cols[rssIndex]), err)
	}
	// vsize is in bytes, rss is in pages.
	rssBytes := rss * int64(os.Getpagesize())
	atomic.StoreInt64(&lastResidentMemorySize, rssBytes)
	return vsize, rssBytes, nil
}

func statMemory(ctx context.Context) (vsize, rss int64, err error) {
	f, err := os.Open("/proc/self/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	return parseStatMemory(ctx, f)
}

func procStats(ctx context.Context) {
	logger := log.FromContext(ctx)

	var m []stats.Measurement

	n, err := numOpenFDs(ctx)
	if err != nil {
		logger.Errorf("failed to get open-fds: %v", err)
	} else {
		m = append(m, openFDs.M(n))
	}
	n, err = numMaxFDs(ctx)
	if err != nil {
		logger.Errorf("failed to get max-fds: %v", err)
	} else {
		m = append(m, maxFDs.M(n))
	}
	vsize, rss, err := statMemory(ctx)
	if err != nil {
		logger.Errorf("failed to get stat: %v", err)
	} else {
		m = append(m,
			virtualMemorySize.M(vsize),
			residentMemorySize.M(rss))
	}
	stats.Record(ctx, m...)
}

func reportProcStats(ctx context.Context) {
	t := time.NewTicker(samplingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			procStats(ctx)
		}
	}
}
