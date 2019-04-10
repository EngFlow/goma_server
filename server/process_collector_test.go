// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package server

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNumOpenFDs(t *testing.T) {
	ctx := context.Background()
	n0, err := numOpenFDs(ctx)
	if err != nil {
		t.Fatalf("numOpenFDs(ctx)=_, %v", err)
	}
	t.Logf("open /dev/null")
	f, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	n, err := numOpenFDs(ctx)
	if err != nil {
		t.Fatalf("numOpenFDs(ctx)=_, %v", err)
	}
	if n != n0+1 {
		t.Errorf("%d != %d + 1", n, n0)
	}
	t.Logf("close")
	f.Close()
	n, err = numOpenFDs(ctx)
	if err != nil {
		t.Fatalf("numOpenFDs(ctx)=_, %v", err)
	}
	if n != n0 {
		t.Errorf("%d != %d", n, n0)
	}
}

func TestParseMaxFDs(t *testing.T) {
	const procSelfLimits = `Limit                     Soft Limit           Hard Limit           Units
Max cpu time              unlimited            unlimited            seconds
Max file size             unlimited            unlimited            bytes
Max data size             unlimited            unlimited            bytes
Max stack size            8388608              unlimited            bytes
Max core file size        0                    unlimited            bytes
Max resident set          unlimited            unlimited            bytes
Max processes             32768                32768                processes
Max open files            32768                32768                files
Max locked memory         unlimited            unlimited            bytes
Max address space         unlimited            unlimited            bytes
Max file locks            unlimited            unlimited            locks
Max pending signals       256886               256886               signals
Max msgqueue size         819200               819200               bytes
Max nice priority         0                    0
Max realtime priority     0                    0
Max realtime timeout      unlimited            unlimited            us
`

	ctx := context.Background()
	n, err := parseMaxFDs(ctx, strings.NewReader(procSelfLimits))
	if err != nil {
		t.Errorf("parseMaxFDs(ctx, procSelfLimits)=_, %v", err)
	}
	if got, want := n, int64(32768); got != want {
		t.Errorf("parseMaxFDs(ctx, procSelfLimits)=%d; want=%d", got, want)
	}
}

func TestParseStatMemory(t *testing.T) {
	const procSelfStat = `9883 (cat) R 19182 9883 19182 34827 9883 4194304 107 0 0 0 0 0 0 0 20 0 1 0 95447658 8044544 169 18446744073709551615 94700269010944 94700269041680 140721631836896 0 0 0 0 0 0 0 0 0 17 13 0 0 0 0 0 94700271139920 94700271141536 94700293513216 140721631845545 140721631845565 140721631845565 140721631850479 0
`
	ctx := context.Background()
	vsize, rss, err := parseStatMemory(ctx, strings.NewReader(procSelfStat))
	if err != nil {
		t.Errorf("parseStatMemory(ctx, procSelfStat)=_, _, %v", err)
	}
	if got, want := vsize, int64(8044544); got != want {
		t.Errorf("parseStatMemroy(ctx, procSelfStat).vss=%d; want=%d", got, want)
	}
	if got, want := rss, int64(169*os.Getpagesize()); got != want {
		t.Errorf("parseStatMemory(ctx, procSelfStat).rss=%d; want=%d", got, want)
	}
}
