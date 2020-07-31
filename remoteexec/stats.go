// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	numRunningOperations = stats.Int64(
		"go.chromium.org/goma/server/remoteexec.running-operations",
		"Number of current running exec operations",
		stats.UnitDimensionless)

	wrapperCount = stats.Int64(
		"go.chromium.org/goma/server/remoteexec.wrapper-counts",
		"Number of requests per wrapper types",
		stats.UnitDimensionless)

	wrapperTypeKey = tag.MustNewKey("wrapper")

	inputBufferAllocSize = stats.Int64(
		"go.chromium.org/goma/server/remoteexec.input-buffer-alloc",
		"Size to allocate buffer for input files",
		stats.UnitBytes)

	allocStatusKey = tag.MustNewKey("status")

	execInventoryTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.exec-inventory",
		"Time in inventory check",
		stats.UnitMilliseconds)
	execInputTreeTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.exec-input-tree",
		"Time in input tree construction",
		stats.UnitMilliseconds)
	execSetupTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.exec-setup",
		"Time in setup",
		stats.UnitMilliseconds)
	execCheckCacheTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.exec-check-cache",
		"Time to check cache",
		stats.UnitMilliseconds)
	execCheckMissingTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.exec-check-missing",
		"Time to check missing",
		stats.UnitMilliseconds)
	execUploadBlobsTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.exec-upload-blobs",
		"Time to upload blobs",
		stats.UnitMilliseconds)
	execExecuteTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.exec-execute",
		"Time to execute",
		stats.UnitMilliseconds)
	execResponseTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.exec-response",
		"Time in response",
		stats.UnitMilliseconds)

	rbeQueueTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.rbe-queue",
		"Time in RBE queue",
		stats.UnitMilliseconds)
	rbeWorkerTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.rbe-worker",
		"Time in RBE worker",
		stats.UnitMilliseconds)
	rbeInputTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.rbe-input",
		"Time in RBE input",
		stats.UnitMilliseconds)
	rbeExecTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.rbe-exec",
		"TIme in RBE exec",
		stats.UnitMilliseconds)
	rbeOutputTime = stats.Float64(
		"go.chromium.org/goma/server/remoteexec.rbe-output",
		"Time in RBE output",
		stats.UnitMilliseconds)

	rbeExitKey                  = tag.MustNewKey("exit")
	rbeCacheKey                 = tag.MustNewKey("cache")
	rbePlatformOSFamilyKey      = tag.MustNewKey("os-family")
	rbePlatformDockerRuntimeKey = tag.MustNewKey("docker-runtime")
	// wrapper?

	rbeTagKeys = []tag.Key{
		rbeExitKey,
		rbeCacheKey,
		rbePlatformOSFamilyKey,
		rbePlatformDockerRuntimeKey,
	}

	defaultLatencyDistribution = view.Distribution(1, 2, 3, 4, 5, 6, 8, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80, 100, 130, 160, 200, 250, 300, 400, 500, 650, 800, 1000, 2000, 5000, 10000, 20000, 50000, 100000, 200000, 500000)

	DefaultViews = []*view.View{
		{
			Description: `Number of current running exec operations`,
			Measure:     numRunningOperations,
			Aggregation: view.Sum(),
		},
		{
			Description: "Number of requests per wrapper types",
			TagKeys: []tag.Key{
				wrapperTypeKey,
			},
			Measure:     wrapperCount,
			Aggregation: view.Count(),
		},
		{
			Description: "Size to allocate buffer for input files",
			TagKeys: []tag.Key{
				allocStatusKey,
			},
			Measure:     inputBufferAllocSize,
			Aggregation: view.Sum(),
		},
		{
			Description: "Time in inventory check",
			Measure:     execInventoryTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time in input tree construction",
			Measure:     execInputTreeTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time in setup",
			Measure:     execSetupTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time to check cache",
			Measure:     execCheckCacheTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time to check missing",
			Measure:     execCheckMissingTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time to upload blobs",
			Measure:     execUploadBlobsTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time to execute",
			Measure:     execExecuteTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time in response",
			Measure:     execResponseTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time in RBE queue",
			Measure:     rbeQueueTime,
			TagKeys:     rbeTagKeys,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time in RBE worker",
			Measure:     rbeWorkerTime,
			TagKeys:     rbeTagKeys,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time in RBE input",
			Measure:     rbeInputTime,
			TagKeys:     rbeTagKeys,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time in RBE exec",
			Measure:     rbeExecTime,
			TagKeys:     rbeTagKeys,
			Aggregation: defaultLatencyDistribution,
		},
		{
			Description: "Time in RBE output",
			Measure:     rbeOutputTime,
			TagKeys:     rbeTagKeys,
			Aggregation: defaultLatencyDistribution,
		},
	}
)

func recordRemoteExecStart(ctx context.Context) {
	stats.Record(ctx, numRunningOperations.M(1))
}

func recordRemoteExecFinish(ctx context.Context) {
	stats.Record(ctx, numRunningOperations.M(-1))
}
