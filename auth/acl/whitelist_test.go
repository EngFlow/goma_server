// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package acl

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"golang.org/x/oauth2"

	"go.chromium.org/goma/server/auth"
)

func TestDefaultWhitelist(t *testing.T) {
	a := ACL{
		Loader: DefaultWhitelist{},
	}
	ctx := context.Background()
	err := a.Update(ctx)
	if err != nil {
		t.Fatal(err)
	}

	testcases := []struct {
		email    string
		audience string
		wantErr  bool
	}{
		{
			email:    "example@google.com",
			audience: GomaClientClientID,
			wantErr:  false,
		},
		{
			email:    "example@chromium.org",
			audience: GomaClientClientID,
			wantErr:  true,
		},
		{
			email:    "goma-client@chrome-infra-auth.iam.gserviceaccount.com",
			audience: "goma-client@chrome-infra-auth.iam.gserviceaccount.com",
			wantErr:  false,
		},
		{
			email:    "me@google.com@example.com",
			audience: GomaClientClientID,
			wantErr:  true,
		},
		{
			email:    "example@google.com",
			audience: "other-xxx.apps.googleusercontent.com",
			wantErr:  true,
		},
	}

	for _, tc := range testcases {
		token := &oauth2.Token{
			AccessToken: fmt.Sprintf("token-value-%s", tc.email),
		}
		_, got, err := a.CheckToken(ctx, token, &auth.TokenInfo{
			Email:    tc.email,
			Audience: tc.audience,
		})
		if tc.wantErr {
			if err == nil {
				t.Errorf("a.CheckToken(ctx, token, %q)=_, _, nil; want err", tc.email)
			}
			continue
		}
		if !reflect.DeepEqual(got, token) {
			t.Errorf("a.CheckToken(ctx, %v, %q)=_, %v, nil; want _, %v, nil", token, tc.email, got, token)
		}
	}
}
