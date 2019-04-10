// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"go.chromium.org/goma/server/auth/enduser"
	authpb "go.chromium.org/goma/server/proto/auth"
)

func TestAuthInfoExpiresAt(t *testing.T) {
	t.Log("expired in 1 second for authInfo.err")
	ai := authInfo{
		err: errors.New("dummy"),
	}
	exp := ai.expiresAt()
	if time.Now().Add(2 * time.Second).Before(exp) {
		t.Errorf("returned ai.expiresAt()=%v; want less than 2 seconds", exp)
	}

	t.Log("expired in 1 second for nil resp")
	ai = authInfo{}
	exp = ai.expiresAt()
	if time.Now().Add(2 * time.Second).Before(exp) {
		t.Errorf("returned ai.expiresAt()=%v; want less than 2 seconds", exp)
	}

	t.Log("expired in 1 second for nil ExpiresAt.")
	ai = authInfo{
		resp: &authpb.AuthResp{},
	}
	exp = ai.expiresAt()
	if time.Now().Add(2 * time.Second).Before(exp) {
		t.Errorf("returned ai.expiresAt()=%v; want less than 2 seconds", exp)
	}

	t.Log("should return the same with ExpiresAt.")
	tm := time.Now().Add(time.Hour)
	expires, err := ptypes.TimestampProto(tm)
	if err != nil {
		t.Fatalf("ptypes.TimestampProto(%v) error %v; want nil", tm, err)
	}
	ai = authInfo{
		resp: &authpb.AuthResp{
			ExpiresAt: expires,
		},
	}
	exp = ai.expiresAt()
	if !tm.Equal(exp) {
		t.Errorf("returned ai.expiresAt()=%v; want %v", exp, tm)
	}
}

func TestAuthInfoCheck(t *testing.T) {
	hourAgo := time.Now().Add(-1 * time.Hour)
	expiredHourAgo, err := ptypes.TimestampProto(hourAgo)
	if err != nil {
		t.Fatalf("ptypes.TimestampProto(%v) error %v; want nil", hourAgo, err)
	}
	hour := time.Now().Add(time.Hour)
	willExpireInHour, err := ptypes.TimestampProto(hour)
	if err != nil {
		t.Fatalf("ptypes.TimestampProto(%v) error %v; want nil", willExpireInHour, err)
	}

	for _, tc := range []struct {
		desc  string
		resp  *authpb.AuthResp
		err   error
		retry int
	}{
		{
			desc: "token has already been expired",
			resp: &authpb.AuthResp{
				ExpiresAt: expiredHourAgo,
				Email:     "example@google.com",
			},
			err:   ErrExpired,
			retry: 1,
		},
		{
			desc: "resp does not have email",
			resp: &authpb.AuthResp{
				ExpiresAt: willExpireInHour,
			},
			err:   ErrInternal,
			retry: 1,
		},
		{
			desc: "quota = 0",
			resp: &authpb.AuthResp{
				ExpiresAt: willExpireInHour,
				Email:     "example@google.com",
				Quota:     0,
			},
			err:   ErrOverQuota,
			retry: 1,
		},
		{
			desc: "unlimited access allowed",
			resp: &authpb.AuthResp{
				ExpiresAt: willExpireInHour,
				Email:     "example@google.com",
				Quota:     -1,
			},
			err:   nil,
			retry: 1,
		},
		{
			desc: "access fail the user used up quota",
			resp: &authpb.AuthResp{
				ExpiresAt: willExpireInHour,
				Email:     "example@google.com",
				Quota:     1,
			},
			err:   ErrOverQuota,
			retry: 2,
		},
		{
			desc: "can access because the user still have enough quota",
			resp: &authpb.AuthResp{
				ExpiresAt: willExpireInHour,
				Email:     "example@google.com",
				Quota:     2,
			},
			err:   nil,
			retry: 1,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var err error
			ai := authInfo{
				resp: tc.resp,
			}
			for i := 0; i < tc.retry; i++ {
				err = ai.Check(context.Background())
			}
			if err != tc.err {
				t.Errorf("ai(%q).Check() retry=%d return error %v; want %v", tc.resp, tc.retry, err, tc.err)
			}
		})
	}
}

func TestAuthExpire(t *testing.T) {
	ctx := context.Background()
	const authorization = "Bearer token-value"
	req := &http.Request{
		URL: &url.URL{
			Path: "/path",
		},
		Header: map[string][]string{
			"Authorization": []string{authorization},
		},
	}
	ch := make(chan chan bool)
	now := time.Now()
	expiresAt := func() time.Time {
		return now.Add(1 * time.Hour)
	}
	expires := expiresAt()
	expiresProto, err := ptypes.TimestampProto(expires)
	if err != nil {
		t.Fatalf("expires %s: %v", expires, err)
	}
	var deadline time.Time
	var callCount int
	a := &Auth{
		Client: dummyClient{
			auth: func(ctx context.Context, req *authpb.AuthReq) (*authpb.AuthResp, error) {
				callCount++
				if req.Authorization != authorization {
					return nil, fmt.Errorf("req.Authorization=%q; want=%q", req.Authorization, authorization)
				}
				return &authpb.AuthResp{
					Email:     "foo@example.com",
					ExpiresAt: expiresProto,
					Quota:     -1,
					GroupId:   "foo",
					Token: &authpb.Token{
						AccessToken: "token-value",
						TokenType:   "Bearer",
					},
				}, nil
			},
		},
		runAt: func(t time.Time, f func()) {
			deadline = t
			rch := <-ch
			f()
			close(rch)
		},
	}

	t.Logf("initial check")
	user, err := a.Check(ctx, req)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	want := enduser.New("foo@example.com", "foo", &oauth2.Token{
		AccessToken: "token-value",
		TokenType:   "Bearer",
	})
	if !reflect.DeepEqual(user, want) {
		t.Errorf("a.Check(ctx, req)=%#v; want=%#v", user, want)
	}

	a.mu.Lock()
	if _, ok := a.cache[authorization]; !ok {
		t.Errorf("%q must exist in cache", authorization)
	}
	a.mu.Unlock()

	if callCount != 1 {
		t.Errorf("call count=%d; want=1", callCount)
	}

	t.Logf("30 minutes later")
	now = now.Add(30 * time.Minute)
	user, err = a.Check(ctx, req)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if !reflect.DeepEqual(user, want) {
		t.Errorf("a.Check(ctx, req)=%#v; want=%#v", user, want)
	}

	if callCount != 1 {
		t.Errorf("call count=%d; want=1", callCount)
	}

	t.Logf("1 hours later")
	now = now.Add(30 * time.Minute)
	rch := make(chan bool)
	// fire runAt
	ch <- rch
	if !deadline.Equal(expiryTime(expires)) {
		t.Errorf("deadline %s != %s (expiresAt %s)", deadline, expiryTime(expires), expires)
	}
	// wait runAt finish.
	<-rch

	a.mu.Lock()
	if _, ok := a.cache[authorization]; ok {
		t.Errorf("%q must be removed in cache", authorization)
	}
	a.mu.Unlock()

	expires2 := expiresAt()
	if otime, ntime := expires, expires2; otime.Equal(ntime) {
		t.Fatalf("expiresAt: %s == %s", otime, ntime)
	}
	expiresProto, err = ptypes.TimestampProto(expires2)
	if err != nil {
		t.Fatalf("timestamp %s: %v", expires2, err)
	}
	user, err = a.Check(ctx, req)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if !reflect.DeepEqual(user, want) {
		t.Errorf("a.Check(ctx, req)=%#v; want=%#v", user, want)
	}
	a.mu.Lock()
	if _, ok := a.cache[authorization]; !ok {
		t.Errorf("%q must exist in cache", authorization)
	}
	a.mu.Unlock()

	if callCount != 2 {
		t.Errorf("call count=%d; want=2", callCount)
	}
}

type dummyClient struct {
	auth func(context.Context, *authpb.AuthReq) (*authpb.AuthResp, error)
}

func (d dummyClient) Auth(ctx context.Context, req *authpb.AuthReq, opts ...grpc.CallOption) (*authpb.AuthResp, error) {
	return d.auth(ctx, req)
}

func TestAuthCheck(t *testing.T) {
	// TODO: better to check the error code?
	// Currently, the test does not check Check returns what error code,
	// but to confirm it actually failed with the expected error, we might
	// need to check the error code?  I am afraid it would be change
	// checker test, though.

	t.Log("0. no Authorization header.")
	a := &Auth{}
	emptyReq := &http.Request{
		// this is needed to make trace work without nil access.
		URL: &url.URL{
			Path: "dummy",
		},
	}
	_, err := a.Check(context.Background(), emptyReq)
	if err != ErrNoAuthHeader {
		t.Errorf("Check(%v) error %v; want %v", emptyReq, err, ErrNoAuthHeader)
	}

	t.Log("1. access succeed (using cache)")
	hour := time.Now().Add(time.Hour)
	willExpireInHour, err := ptypes.TimestampProto(hour)
	if err != nil {
		t.Fatalf("ptypes.TimestampProto(%v) error %v; want nil", willExpireInHour, err)
	}
	email := "example@google.com"
	a1 := &Auth{
		cache: map[string]*authInfo{
			"Bearer test": &authInfo{
				resp: &authpb.AuthResp{
					Email:     email,
					ExpiresAt: willExpireInHour,
					Quota:     -1,
					Token: &authpb.Token{
						AccessToken: "test",
						TokenType:   "Bearer",
					},
				},
			},
		},
	}
	testReq := &http.Request{
		Header: map[string][]string{
			"Authorization": []string{"Bearer test"},
		},
		// this is needed to make trace work without nil access.
		URL: &url.URL{
			Path: "dummy",
		},
	}
	eu, err := a1.Check(context.Background(), testReq)
	if err != nil {
		t.Errorf("Check(%v) error %v; want nil", testReq, err)
	}
	expectedEu := enduser.New(email, "", &oauth2.Token{
		AccessToken: "test",
		TokenType:   "Bearer",
	})
	if !reflect.DeepEqual(eu, expectedEu) {
		t.Errorf("Check(%v)=%v; want %v", testReq, eu, expectedEu)
	}

	t.Log("2. access succeed (using Auth client)")
	a2 := &Auth{
		Client: dummyClient{
			auth: func(ctx context.Context, req *authpb.AuthReq) (*authpb.AuthResp, error) {
				return &authpb.AuthResp{
					Email:     email,
					ExpiresAt: willExpireInHour,
					Quota:     -1,
					Token: &authpb.Token{
						AccessToken: "test",
						TokenType:   "Bearer",
					},
				}, nil
			},
		},
	}
	eu, err = a2.Check(context.Background(), testReq)
	if err != nil {
		t.Errorf("Check(%v) error %v; want nil", testReq, err)
	}
	if !reflect.DeepEqual(eu, expectedEu) {
		t.Errorf("Check(%v)=%v; want %v", testReq, eu, expectedEu)
	}

	t.Log("3. access fail due to fail to fetch from Auth client.")
	a3 := &Auth{
		Client: dummyClient{
			auth: func(ctx context.Context, req *authpb.AuthReq) (*authpb.AuthResp, error) {
				return nil, grpc.Errorf(codes.Internal, "auth server error")
			},
		},
	}
	_, err = a3.Check(context.Background(), testReq)
	if err == nil {
		t.Errorf("Check(%v) nil error; want error", testReq)
	}

	t.Log("4. access fail due to Quota = 0.")
	a4 := &Auth{
		Client: dummyClient{
			auth: func(ctx context.Context, req *authpb.AuthReq) (*authpb.AuthResp, error) {
				return &authpb.AuthResp{
					Email:     email,
					ExpiresAt: willExpireInHour,
					Quota:     0,
					Token: &authpb.Token{
						AccessToken: "test",
						TokenType:   "Bearer",
					},
				}, nil
			},
		},
	}
	_, err = a4.Check(context.Background(), testReq)
	if err == nil {
		t.Errorf("Check(%v) nil error; want error", testReq)
	}

	t.Log("5. access fail due to expired token.")
	hourAgo := time.Now().Add(-1 * time.Hour)
	expiredHourAgo, err := ptypes.TimestampProto(hourAgo)
	a5 := &Auth{
		Client: dummyClient{
			auth: func(ctx context.Context, req *authpb.AuthReq) (*authpb.AuthResp, error) {
				return &authpb.AuthResp{
					Email:     email,
					ExpiresAt: expiredHourAgo,
					Quota:     -1,
					Token: &authpb.Token{
						AccessToken: "test",
						TokenType:   "Bearer",
					},
				}, nil
			},
		},
	}
	_, err = a5.Check(context.Background(), testReq)
	if err == nil {
		t.Errorf("Check(%v) nil error; want error", testReq)
	}
}

func TestAuthInfoString(t *testing.T) {
	ai := &authInfo{
		resp: &authpb.AuthResp{
			Email:   "someone@google.com",
			GroupId: "somegroup",
		},
	}
	if got := fmt.Sprint(ai); strings.Contains(got, "someone@google.com") {
		t.Errorf("fmt.Sprint(ai)=%s; leak email address", got)
	}
	if got := fmt.Sprintf("%[1]s %[1]v %+[1]v %#[1]v %[1]q", ai); strings.Contains(got, "someone@google.com") {
		t.Errorf(`fmt.Sprintf("...", ai)=%s; leak email address`, got)
	}
}
