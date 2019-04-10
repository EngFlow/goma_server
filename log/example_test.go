// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package log_test

import (
	"context"

	"go.opencensus.io/tag"
	"go.uber.org/zap"

	"go.chromium.org/goma/server/log"
)

func Example() {
	ctx := context.Background()
	log.SetZapLogger(zap.NewExample())
	logger := log.FromContext(ctx)
	defer logger.Sync()
	k, err := tag.NewKey("go.chromium.org/goma/server/log/example")
	if err != nil {
		logger.Fatal(err)
	}
	log.RegisterTagKey(k)

	ctx, err = tag.New(ctx, tag.Insert(k, "trace_1"))
	if err != nil {
		logger.Fatal(err)
	}
	logger = log.FromContext(ctx)

	logger.Info("info")
	// Output:
	// {"level":"info","msg":"info","go.chromium.org/goma/server/log/example":"trace_1"}
}
