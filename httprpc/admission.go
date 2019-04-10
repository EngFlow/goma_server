// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package httprpc

import (
	"net/http"

	"go.chromium.org/goma/server/log"
)

// AdmissionController checks incoming request.
type AdmissionController interface {
	Admit(*http.Request) error
}

// AdmissionControl adds admission controller to h.
func AdmissionControl(ac AdmissionController, h http.Handler) http.Handler {
	if ac == nil {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		err := ac.Admit(req)
		if err != nil {
			code, msg := httpStatus(err)
			http.Error(w, msg, code)
			logger := log.FromContext(ctx)
			logger.Errorf("deny %s: %d %s: %v", req.URL.Path, code, msg, err)
			return
		}
		h.ServeHTTP(w, req)
	})
}
