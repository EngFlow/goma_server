// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package httprpc

import (
	"net/http"

	"go.opencensus.io/trace"
)

// Trace adds labels to trace span for requested path.
// It would be used as top handler for incoming request under ochttp.Handler.
func Trace(handler http.Handler, labels map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		span := trace.FromContext(req.Context())
		var attrs []trace.Attribute
		for k, v := range labels {
			if v == "" {
				continue
			}
			attrs = append(attrs, trace.StringAttribute(k, v))
		}
		span.AddAttributes(attrs...)
		handler.ServeHTTP(w, req)
	})
}
