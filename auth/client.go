// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.opencensus.io/trace"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"

	"go.chromium.org/goma/server/auth/enduser"
	"go.chromium.org/goma/server/log"
	authpb "go.chromium.org/goma/server/proto/auth"
	"go.chromium.org/goma/server/rpc"
)

// ErrNoAuthHeader represents authentication failure due to lack of Authorization header in an HTTP request.
var ErrNoAuthHeader = errors.New("no Authorization header")

// ErrInternal represents internal error.
var ErrInternal = errors.New("internal error")

// ErrExpired represents expiration of access token.
var ErrExpired = errors.New("expired")

// ErrOverQuota represents the user used up the quota.
var ErrOverQuota = errors.New("over quota")

type authInfo struct {
	err error
	mu  sync.Mutex // protect resp.Quota

	// TODO: define type to avoid email leak in logging.
	resp *authpb.AuthResp
}

func (ai *authInfo) expiresAt() time.Time {
	if ai.err != nil || ai.resp == nil || ai.resp.ExpiresAt == nil {
		return time.Now().Add(1 * time.Second)
	}
	return time.Unix(ai.resp.ExpiresAt.Seconds, int64(ai.resp.ExpiresAt.Nanos))
}

func (ai *authInfo) String() string {
	return fmt.Sprintf("authInfo:%s %s %s err:%v", ai.resp.GetGroupId(), ai.expiresAt(), ai.resp.GetErrorDescription(), ai.err)
}

// Check returns error if authInfo does not have valid AuthResp.
// This code also checks quota, and decrease remaining quota for accessing
// the service.
func (ai *authInfo) Check(ctx context.Context) error {
	logger := log.FromContext(ctx)
	// check order:
	// 1. authInfo.err == nil?
	// 2. ErrorDescription
	// 3. Email exists.
	// 4. token expiration.
	// 5. Quota > 0 || Quota < 0 (unlimited)
	if ai.err != nil {
		logger.Warnf("auth.Check %v due to %v", ErrInternal, ai.err)
		return ErrInternal
	}
	if ai.resp.ErrorDescription != "" {
		logger.Warnf("permission denied: %s", ai.resp.ErrorDescription)
		return errors.New(ai.resp.ErrorDescription)
	}
	// Valid AuthResp should not make Email empty but it is.
	if ai.resp.Email == "" {
		logger.Warnf("auth.Check %v", ErrInternal)
		return ErrInternal
	}

	expiresAt := ai.expiresAt()
	now := time.Now()
	if expiresAt.Before(now) {
		logger.Warnf("auth.Check %v because token was expired at %v", ErrExpired, expiresAt)
		return ErrExpired
	}

	ai.mu.Lock()
	defer ai.mu.Unlock()
	if ai.resp.Quota < 0 { // unlimited.
		return nil
	}
	if ai.resp.Quota == 0 {
		return ErrOverQuota
	}
	ai.resp.Quota--
	return nil
}

type Auth struct {
	Client authpb.AuthServiceClient
	Retry  rpc.Retry

	sg    singleflight.Group
	mu    sync.Mutex
	cache map[string]*authInfo

	runAt func(time.Time, func())
}

func (a *Auth) scheduledRun(t time.Time, f func()) {
	scheduledRun := a.runAt
	if scheduledRun == nil {
		scheduledRun = runAt
	}
	scheduledRun(t, f)
}

// Check checks authorization header in an HTTP request.
// The function returns error if authentication failed.
// ErrNoAuthHeader is returned if no authorization header is in the request.
func (a *Auth) Check(ctx context.Context, req *http.Request) (*enduser.EndUser, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/auth.Auth.Check")
	defer span.End()
	logger := log.FromContext(ctx)

	authorization := req.Header.Get("Authorization")
	if authorization == "" {
		logger.Warnf("no authorization header")
		return nil, ErrNoAuthHeader
	}
	a.mu.Lock()
	if a.cache == nil {
		a.cache = make(map[string]*authInfo)
	}
	ai, ok := a.cache[authorization]
	a.mu.Unlock()
	if !ok {
		v, err, _ := a.sg.Do(authorization, func() (interface{}, error) {
			logger.Debugf("first call for %s...", authorization[:len(authorization)/3])
			ai := &authInfo{}
			err := a.Retry.Do(ctx, func() error {
				var err error
				ai.resp, err = a.Client.Auth(ctx, &authpb.AuthReq{
					Authorization: authorization,
				})
				return err
			})
			if err != nil {
				logger.Errorf("auth failed: %v", err)
				ai.err = err
			}
			go a.scheduledRun(expiryTime(ai.expiresAt()), func() {
				a.mu.Lock()
				delete(a.cache, authorization)
				a.mu.Unlock()
			})
			a.mu.Lock()
			a.cache[authorization] = ai
			a.mu.Unlock()
			return ai, nil
		})
		if err != nil {
			logger.Errorf("auth error: %v", err)
			return nil, err
		}
		ai = v.(*authInfo)
	}
	err := ai.Check(ctx)
	if err != nil {
		logger.Warnf("auth check err: %v", err)
		return nil, err
	}
	token := &oauth2.Token{
		AccessToken: ai.resp.Token.GetAccessToken(),
		TokenType:   ai.resp.Token.GetTokenType(),
	}
	return enduser.New(ai.resp.Email, ai.resp.GroupId, token), nil
}

// Auth authenticates the requests and returns new context with enduser info.
func (a *Auth) Auth(ctx context.Context, req *http.Request) (context.Context, error) {
	u, err := a.Check(ctx, req)
	if err != nil {
		return ctx, err
	}
	return enduser.NewContext(ctx, u), nil
}
