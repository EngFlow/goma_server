// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package exec

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
)

var (
	apiErrors = stats.Int64(
		"go.chromium.org/goma/server/exec.api-error",
		"exec request api-error",
		stats.UnitDimensionless)

	apiErrorKey = mustTagNewKey("api-error")

	// DefaultViews are the default views provided by this package.
	// You need to register the view for data to actually be collected.
	DefaultViews = []*view.View{
		&view.View{
			Description: "exec request api-error",
			TagKeys: []tag.Key{
				apiErrorKey,
			},
			Measure:     apiErrors,
			Aggregation: view.Count(),
		},
		&view.View{
			Description: `counts toolchain selection. result is "used", "found", "requested" or "missed"`,
			TagKeys: []tag.Key{
				selectorKey,
				resultKey,
			},
			Measure:     toolchainSelects,
			Aggregation: view.Count(),
		},
	}
)

func mustTagNewKey(name string) tag.Key {
	k, err := tag.NewKey(name)
	if err != nil {
		logger := log.FromContext(context.Background())
		logger.Fatal(err)
	}
	return k
}

func apiErrorValue(resp *gomapb.ExecResp) string {
	if errVal := resp.GetError(); errVal != gomapb.ExecResp_OK {
		// marked as BAD_REQUEST for
		// - compiler/subprogram not found
		// - bad path_type in command config
		// - input root detection failed
		return errVal.String()
	}
	if len(resp.ErrorMessage) > 0 {
		return "internal"
	}
	if len(resp.MissingInput) > 0 {
		return "missing-inputs"
	}
	return "OK"
}

// RecordAPIError records api-error in resp.
func RecordAPIError(ctx context.Context, resp *gomapb.ExecResp) error {
	ctx, err := tag.New(ctx, tag.Upsert(apiErrorKey, apiErrorValue(resp)))
	if err != nil {
		return err
	}
	stats.Record(ctx, apiErrors.M(1))
	return nil
}
