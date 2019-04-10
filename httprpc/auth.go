// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package httprpc

import (
	"context"
	"fmt"
	"net/http"

	"go.chromium.org/goma/server/auth/enduser"
	"go.chromium.org/goma/server/log"
)

// AuthChecker represents an interface to checks HTTP access.
type AuthChecker interface {
	// Check represents the function to check HTTP access.
	// If the access is granted, it returns non-nil enduser.EndUser instance.
	Check(context.Context, *http.Request) (*enduser.EndUser, error)
}

// AuthHandler converts given http.Handler to access controlled HTTP handler using AuthChecker.
// Alternatives: WithAuth handler option if it requires retry with Unauthenticated error.
func AuthHandler(a AuthChecker, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		u, err := a.Check(ctx, req)
		if err != nil {
			code := http.StatusUnauthorized
			http.Error(w, fmt.Sprintf("auth failed %s: %v", RemoteAddr(req), err), code)
			logger := log.FromContext(ctx)
			logger.Errorf("auth error %s: %d %s: %v", req.URL.Path, code, http.StatusText(code), err)
			return
		}
		req = req.WithContext(enduser.NewContext(ctx, u))
		h.ServeHTTP(w, req)
	})
}
