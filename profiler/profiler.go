// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package profiler provides convenient function to enable cloud profiler.
package profiler

import (
	"context"
	"os"
	"path/filepath"

	gce "cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/profiler"
	"google.golang.org/api/option"
	"google.golang.org/grpc"

	"go.chromium.org/goma/server/log"
)

// TODO: profiler API over quota? http://b/73749051

// Setup starts cloud profiler if executable is running on GCE.
func Setup(ctx context.Context) {
	if gce.OnGCE() {
		logger := log.FromContext(ctx)
		cluster, err := gce.InstanceAttributeValue("cluster-name")
		if err != nil {
			logger.Errorf("failed to get cluster name: %v", err)
			cluster = "unknown"
		}
		target := cluster + "." + filepath.Base(os.Args[0])
		logger.Infof("profiler target name: %s", target)
		err = profiler.Start(profiler.Config{Service: target},
			// Disallow grpc in google-api-go-client to send stats/trace of profiler grpc's api call.
			option.WithGRPCDialOption(grpc.WithStatsHandler(nil)))
		if err != nil {
			logger.Errorf("failed to start cloud profiler: %v", err)
		}
	}
}
