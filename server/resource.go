// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package server

import (
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
)

// MemoryLimit returns server memory limit, set by cgroup.
func MemoryLimit() (int64, error) {
	const fname = "/sys/fs/cgroup/memory/memory.limit_in_bytes"
	buf, err := ioutil.ReadFile(fname)
	if err != nil {
		return 0, err
	}
	data := strings.TrimSpace(string(buf))
	v, err := strconv.ParseInt(data, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse %s: %q: %v", fname, data, err)
	}
	return v, nil
}
