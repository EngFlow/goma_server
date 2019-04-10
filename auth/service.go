// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package auth

import (
	"context"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes"
	"go.opencensus.io/trace"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/log"
	authpb "go.chromium.org/goma/server/proto/auth"
)

type tokenCacheEntry struct {
	*TokenInfo

	Group string
	Token *oauth2.Token
}

func (te *tokenCacheEntry) TokenProto() *authpb.Token {
	if te.Token == nil {
		return &authpb.Token{}
	}
	return &authpb.Token{
		AccessToken: te.Token.AccessToken,
		TokenType:   te.Token.TokenType,
	}
}

// Service implements goma auth service.
type Service struct {
	// CheckToken optionally checks access token with token info.
	// If it is not set, all access will be rejected.
	// If it returns grpc's codes.PermissionDenied error,
	// error message will be used as ErrorDescription for user.
	CheckToken func(context.Context, *oauth2.Token, *TokenInfo) (string, *oauth2.Token, error)

	sg         singleflight.Group
	mu         sync.Mutex
	tokenCache map[string]*tokenCacheEntry
	fetchInfo  func(context.Context, *oauth2.Token) (*TokenInfo, error)
	runAt      func(time.Time, func())
}

func (s *Service) fetch(ctx context.Context, token *oauth2.Token) (*TokenInfo, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/auth.fetch")
	defer span.End()
	fetchInfo := s.fetchInfo
	if fetchInfo == nil {
		fetchInfo = fetch
	}
	return fetchInfo(ctx, token)
}

func (s *Service) checkToken(ctx context.Context, token *oauth2.Token, tokenInfo *TokenInfo) (string, *oauth2.Token, error) {
	if s.CheckToken == nil {
		return "", nil, grpc.Errorf(codes.Internal, "CheckToken is not configured")
	}
	return s.CheckToken(ctx, token, tokenInfo)
}

func (s *Service) scheduledRun(t time.Time, f func()) {
	scheduledRun := s.runAt
	if scheduledRun == nil {
		scheduledRun = runAt
	}
	scheduledRun(t, f)
}

// Auth checks authorization header of incoming request, and
// replies end user information.
//
// TODO: find answers to following questions.
// 1. can auth server return expired token? (currently yes)
// 2. should auth server refresh expired token? (currently no)
// 3. should grpc status code represent status of request or access token?
// 4. how error description should be handled?
//    currently, it is stored in cache but not used by anybody.
// 5. should auth server create go routine for each token to expire the entry?
//    (currently yes)
// 6. how do we implement quota?
// 7. how do we integrate auth server with chrome-infra-auth?
func (s *Service) Auth(ctx context.Context, req *authpb.AuthReq) (*authpb.AuthResp, error) {
	logger := log.FromContext(ctx)
	token, err := parseToken(req.Authorization)
	if err != nil {
		logger.Errorf("parse token failure %s: %v", req.Authorization, err)
		return nil, grpc.Errorf(codes.InvalidArgument, "wrong authorization: %v", err)
	}

	// TODO: factor out singleflight timed cache.
	s.mu.Lock()
	if s.tokenCache == nil {
		s.tokenCache = make(map[string]*tokenCacheEntry)
	}
	k := tokenKey(token)
	te, ok := s.tokenCache[k]
	s.mu.Unlock()
	if !ok {
		v, err, _ := s.sg.Do(k, func() (interface{}, error) {
			te := &tokenCacheEntry{}
			var err error
			te.TokenInfo, err = s.fetch(ctx, token)
			if err != nil {
				te.TokenInfo = &TokenInfo{
					Err: err,
					// set 1 second negative cache.
					ExpiresAt: time.Now().Add(1 * time.Second),
				}
			}
			if te.TokenInfo.Err == nil {
				te.Group, te.Token, err = s.checkToken(ctx, token, te.TokenInfo)
				if err != nil {
					te.TokenInfo.Err = err
				}
				if te.Token != nil && !te.Token.Expiry.IsZero() && te.Token.Expiry.Before(te.TokenInfo.ExpiresAt) {
					te.TokenInfo.ExpiresAt = te.Token.Expiry
				}
			}
			go s.scheduledRun(expiryTime(te.TokenInfo.ExpiresAt), func() {
				s.mu.Lock()
				delete(s.tokenCache, k)
				s.mu.Unlock()
			})
			s.mu.Lock()
			s.tokenCache[k] = te
			s.mu.Unlock()
			return te, nil
		})
		if err != nil {
			logger.Errorf("auth error: %v", err)
			return nil, grpc.Errorf(codes.Internal, "auth error: %v", err)
		}
		te = v.(*tokenCacheEntry)
	}
	if te.TokenInfo == nil {
		return nil, grpc.Errorf(codes.Internal, "nil TokenInfo is given")
	}

	expires, err := ptypes.TimestampProto(te.TokenInfo.ExpiresAt)
	if err != nil {
		return nil, grpc.Errorf(codes.OutOfRange, "bad ExpiresAt %s: %v", te.TokenInfo.ExpiresAt, err)
	}

	var errorDescription string
	var quota int32
	if te.TokenInfo.Err == nil {
		quota = -1 // TODO: -1 is unlimited.
	} else if st, ok := status.FromError(te.TokenInfo.Err); ok {
		switch st.Code() {
		case codes.OK:
			quota = -1 // TODO: -1 is unlimited.
			logger.Infof("token info error non-nil, but ok?: %v", st.Message())
		case codes.PermissionDenied:
			errorDescription = st.Message()
			logger.Errorf("token permission denied: %v", te.TokenInfo.Err)
		default:
			errorDescription = "internal error"
			logger.Errorf("token info error: %v", te.TokenInfo.Err)
		}
	} else {
		// non grpc error.
		errorDescription = "internal error"
		logger.Errorf("token info error: %v", te.TokenInfo.Err)
	}

	resp := &authpb.AuthResp{
		Email:            te.TokenInfo.Email,
		ExpiresAt:        expires,
		Quota:            quota,
		ErrorDescription: errorDescription,
		GroupId:          te.Group,
		Token:            te.TokenProto(),
	}

	return resp, nil
}
