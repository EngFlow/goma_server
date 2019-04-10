// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package httprpc

import (
	"net/http"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCanonicalCode(t *testing.T) {
	// go/http-canonical-mapping

	// make sure grpc status code round back.
	// commented out codes don't have 1:1 mapping.
	for _, code := range []codes.Code{
		codes.OK,
		codes.Canceled,
		// codes.Unknown,
		codes.InvalidArgument,
		codes.DeadlineExceeded,
		codes.NotFound,
		// codes.AlreadyExists,
		codes.PermissionDenied,
		codes.ResourceExhausted,
		// codes.FailedPrecondition,
		codes.Aborted,
		// codes.OutOfRange,
		codes.Unimplemented,
		codes.Internal,
		codes.Unavailable,
		// codes.DataLoss,
		// codes.Unauthenticated,
	} {
		hc, _ := httpStatus(status.Errorf(code, "%v", code))
		got := fromHTTPStatus(hc)
		if got != code {
			t.Errorf("%d %s != %d %s (via %d %s)", got, got, code, code, hc, http.StatusText(hc))
		}
	}

	for _, tc := range []struct {
		from, to codes.Code
		hc       int
	}{
		{
			from: codes.Unknown,
			to:   codes.Internal,
			hc:   http.StatusInternalServerError,
		},
		{
			from: codes.AlreadyExists,
			to:   codes.Aborted,
			hc:   http.StatusConflict,
		},
		{
			from: codes.FailedPrecondition,
			to:   codes.InvalidArgument,
			hc:   http.StatusBadRequest,
		},
		{
			from: codes.OutOfRange,
			to:   codes.InvalidArgument,
			hc:   http.StatusBadRequest,
		},
		{
			from: codes.DataLoss,
			to:   codes.Internal,
			hc:   http.StatusInternalServerError,
		},
		{
			// goma specific.
			from: codes.Unauthenticated,
			to:   codes.Internal,
			hc:   http.StatusInternalServerError,
		},
	} {
		gothc, _ := httpStatus(status.Errorf(tc.from, "%v", tc.from))
		got := fromHTTPStatus(gothc)
		if gothc != tc.hc || got != tc.to {
			t.Errorf("%d %s => (%d %s) => %d %s; want (%d %s) => %d %s", tc.from, tc.from, gothc, http.StatusText(gothc), got, got, tc.hc, http.StatusText(tc.hc), tc.to, tc.to)
		}
	}
}
