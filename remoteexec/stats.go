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
	}
)

func recordRemoteExecStart(ctx context.Context) {
	stats.Record(ctx, numRunningOperations.M(1))
}

func recordRemoteExecFinish(ctx context.Context) {
	stats.Record(ctx, numRunningOperations.M(-1))
}
