// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package httprpc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"go.chromium.org/goma/server/auth/enduser"
)

type fakeAuth struct {
	check func(ctx context.Context, req *http.Request) (*enduser.EndUser, error)
}

func (s fakeAuth) Check(ctx context.Context, req *http.Request) (*enduser.EndUser, error) {
	return s.check(ctx, req)
}

func TestAuthHandler(t *testing.T) {
	for _, tc := range []struct {
		desc string
		f    fakeAuth
		want int
	}{
		{
			desc: "AuthHandler should allow access on auth success",
			f: fakeAuth{
				check: func(ctx context.Context, req *http.Request) (*enduser.EndUser, error) {
					return enduser.New("dummy@example.com", "dummy", &oauth2.Token{}), nil
				},
			},
			want: http.StatusOK,
		},
		{
			desc: "AuthHandler must deny access on auth fail",
			f: fakeAuth{
				check: func(ctx context.Context, req *http.Request) (*enduser.EndUser, error) {
					return nil, grpc.Errorf(codes.PermissionDenied, "permission denied")
				},
			},
			want: http.StatusUnauthorized,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ts := httptest.NewServer(AuthHandler(tc.f, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintln(w, "Hello, client")
			})))
			defer ts.Close()
			res, err := http.Get(ts.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.want {
				t.Errorf("res.StatusCode=%d; want %d", res.StatusCode, http.StatusOK)
			}
		})
	}
}
