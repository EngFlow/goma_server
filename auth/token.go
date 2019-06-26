// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/rpc"
)

// expiryDelta is how earlier a token is considered expired than
// its actual expiration time.  same as
// https://github.com/golang/oauth2/blob/master/token.go
const expiryDelta = 10 * time.Second

func expiryTime(t time.Time) time.Time {
	return t.Round(0).Add(-expiryDelta)
}

// TokenInfo represents access token's info.
type TokenInfo struct {
	// Email is email address associated with the access token.
	Email string

	// Audience is OAuth2 client_id of the access token.
	Audience string

	// ExpiresAt is expirary timestamp of the access token.
	ExpiresAt time.Time

	// Err represents error of access token.
	Err error
}

// parseToken parses authorization header and extracts oauth2 token.
func parseToken(auth string) (*oauth2.Token, error) {
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, fmt.Errorf("not bearer authorization header: %q", auth)
	}
	return &oauth2.Token{
		AccessToken: strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")),
		TokenType:   "Bearer",
	}, nil
}

func tokenKey(token *oauth2.Token) string {
	return fmt.Sprintf("%s %s", token.TokenType, token.AccessToken)
}

const tokeninfoURL = "https://oauth2.googleapis.com/tokeninfo?access_token="

func trimAccessTokenInErr(err error, accessToken string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), accessToken) {
		return errors.New(strings.Replace(err.Error(), accessToken, accessToken[:len(accessToken)/3], -1) + "...")
	}
	return err
}

// Fetch fetches access token info.
func fetch(ctx context.Context, token *oauth2.Token) (*TokenInfo, error) {
	req, err := http.NewRequest("GET", tokeninfoURL+url.QueryEscape(token.AccessToken), nil)
	if err != nil {
		return nil, trimAccessTokenInErr(err, token.AccessToken)
	}

	var tokenInfo *TokenInfo
	err = rpc.Retry{}.Do(ctx, func() error {
		// always use 1 min timeout regardless of incoming context.
		// retry is controlled by incoming context.
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()
		req = req.WithContext(ctx)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return status.Errorf(codes.Unavailable, "fetch error: %v", trimAccessTokenInErr(err, token.AccessToken))
		}
		defer resp.Body.Close()
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return status.Errorf(codes.Internal, "read tokeninfo response: code=%d %v", resp.StatusCode, err)
		}
		tokenInfo, err = parseResp(data)
		if err != nil {
			return status.Errorf(codes.Internal, "parse tokeninfo response: code=%d %v", resp.StatusCode, err)
		}
		return nil
	})
	return tokenInfo, err
}

func parseResp(data []byte) (*TokenInfo, error) {
	var js struct {
		Email            string `json:"email"`
		Audience         string `json:"aud"`
		ExpiresAt        string `json:"exp"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	err := d.Decode(&js)
	if err != nil {
		return nil, err
	}
	if js.Email == "" && js.ExpiresAt == "" && js.Error == "" && js.ErrorDescription == "" {
		return nil, status.Errorf(codes.Internal, "unexpected token info: %v", js)
	}
	ti := &TokenInfo{
		Email:    js.Email,
		Audience: js.Audience,
	}
	if js.Error != "" || js.ErrorDescription != "" {
		ti.Err = status.Errorf(codes.PermissionDenied, "%s: %q", js.Error, js.ErrorDescription)
	} else {
		// parse exp iff no error is set.
		// when error exists, maybe no exp.
		ts, err := strconv.ParseInt(js.ExpiresAt, 10, 64)
		if err != nil {
			ti.Err = status.Errorf(codes.Internal, "parse exp: %v", err)
		} else {
			ti.ExpiresAt = time.Unix(ts, 0)
		}
	}
	return ti, nil
}

func runAt(t time.Time, f func()) {
	time.Sleep(t.Sub(time.Now()))
	f()
}
