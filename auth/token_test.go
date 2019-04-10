// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package auth

import (
	"reflect"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestParseToken(t *testing.T) {
	testcases := []struct {
		input     string
		want      *oauth2.Token
		wantError bool
	}{
		{
			input: "Bearer mF_9.B5f-4.1JqM",
			want: &oauth2.Token{
				AccessToken: "mF_9.B5f-4.1JqM",
				TokenType:   "Bearer",
			},
		},
		{
			input:     "invalid",
			wantError: true,
		},
	}

	for _, tc := range testcases {
		got, err := parseToken(tc.input)
		if tc.wantError && err == nil {
			t.Errorf("Parse(%s)=_, nil; want error", tc.input)
		}
		if !tc.wantError && err != nil {
			t.Errorf("Parse(%s)=_, %v; want nil", tc.input, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Parse(%s)=%s; want %s", tc.input, got, tc.want)
		}
	}
}

func TestParseResponse(t *testing.T) {
	testcases := []struct {
		input              []byte
		want               *TokenInfo
		wantError          bool
		wantTokenInfoError codes.Code
	}{
		{
			input:     nil,
			wantError: true,
		},
		{
			input:     []byte(""),
			wantError: true,
		},
		{
			input:     []byte("{}"),
			wantError: true,
		},
		{
			input: []byte(`{
				"email": "user@example.org",
				"exp": "not_a_number"
			}`),
			wantTokenInfoError: codes.Internal,
		},
		{
			input: []byte(`{
				"email": "user@example.org",
				"exp": "12345"
			}`),
			want: &TokenInfo{
				Email:     "user@example.org",
				ExpiresAt: time.Unix(12345, 0),
			},
		},
		{
			input: []byte(`{
				"email": "user@example.org",
				"error_description": "this is error."
			}`),
			wantTokenInfoError: codes.PermissionDenied,
		},
	}
	for _, tc := range testcases {
		ti, err := parseResp(tc.input)
		if tc.wantError {
			if err == nil {
				t.Errorf("parseResp(%s)=_, nil; want error", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseResp(%s)=_, %v; want nil", tc.input, err)
		}
		if tc.wantTokenInfoError != codes.OK {
			st, _ := status.FromError(ti.Err)
			if st.Code() != tc.wantTokenInfoError {
				t.Errorf("parseResp(%s) .Err=%v; want=%v", tc.input, ti.Err, tc.wantTokenInfoError)
			}
			continue
		}
		if !reflect.DeepEqual(ti, tc.want) {
			t.Errorf("parseResp(%s)=%s; want %s", tc.input, ti, tc.want)
		}
	}
}
