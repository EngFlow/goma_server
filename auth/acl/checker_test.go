// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package acl

import (
	"context"
	"testing"

	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/auth"
	"go.chromium.org/goma/server/auth/account"
	pb "go.chromium.org/goma/server/proto/auth"
)

type fakePool struct{}

func (fakePool) New(name string) (account.Account, error) {
	return fakeAccount{name}, nil
}

type fakeAccount struct {
	name string
}

func (f fakeAccount) Equals(other account.Account) bool {
	o, ok := other.(fakeAccount)
	if !ok {
		return false
	}
	return f.name != o.name
}

func (f fakeAccount) Token(ctx context.Context) (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: "token"}, nil
}

func TestChecker(t *testing.T) {
	config := &pb.ACL{
		Groups: []*pb.Group{
			{
				Id:     "service-account",
				Emails: []string{"foo@project.iam.gserviceaccount.com"},
			},
			{
				Id:             "chrome-bot",
				Emails:         []string{"goma-client@chrome-infra-auth.iam.gserviceaccount.com"},
				ServiceAccount: "chrome-bot-service-account",
			},
			{
				Id:             "googler",
				Audience:       "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
				Domains:        []string{"google.com"},
				ServiceAccount: "googler-service-account",
			},
			{
				Id:             "contributor",
				Audience:       "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
				Emails:         []string{"foo@gmail.com"},
				ServiceAccount: "contributor-service-account",
			},
		},
	}

	checker := &Checker{
		Pool: fakePool{},
	}

	ctx := context.Background()
	err := checker.Set(ctx, config)
	if err != nil {
		t.Errorf("checker.Set(ctx, config)=%v; want nil-error", err)
	}

	type testData struct {
		tokenInfo *auth.TokenInfo
		errCode   codes.Code
	}

	testCases := map[string]*testData{
		"service-account": {
			tokenInfo: &auth.TokenInfo{
				Email:    "foo@project.iam.gserviceaccount.com",
				Audience: "123456-xxxxxx.apps.googleusercontent.com",
			},
		},
		"chrome-bot": {
			tokenInfo: &auth.TokenInfo{
				Email:    "goma-client@chrome-infra-auth.iam.gserviceaccount.com",
				Audience: "7890-xxxxxx.apps.googleusercontent.com",
			},
		},
		"googler": {
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
		},
		"malicious-googler": {
			tokenInfo: &auth.TokenInfo{
				Email:    "malicious@google.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
		},
		"contributor": {
			tokenInfo: &auth.TokenInfo{
				Email:    "foo@gmail.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
		},
		"unknown service account": {
			tokenInfo: &auth.TokenInfo{
				Email:    "unknown-service-account@chrome-infra-auth.iam.gserviceaccount.com",
				Audience: "7890-xxxxxx.apps.googleusercontent.com",
			},
			errCode: codes.PermissionDenied,
		},
		"unknown audience": {
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "187410957766-f28toeml294bk5mm7nr2jn2rup1rvtjj.apps.googleusercontent.com",
			},
			errCode: codes.PermissionDenied,
		},
		"unknown user": {
			tokenInfo: &auth.TokenInfo{
				Email:    "unknown.user@gmail.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
			errCode: codes.PermissionDenied,
		},
		"contributor2": {
			tokenInfo: &auth.TokenInfo{
				Email:    "contributor@gmail.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
			errCode: codes.PermissionDenied,
		},
	}

	testCheck := func() {
		for desc, tc := range testCases {
			t.Logf("check %s %s", desc, tc.tokenInfo.Email)
			_, _, err := checker.CheckToken(ctx, &oauth2.Token{AccessToken: "token"}, tc.tokenInfo)
			if st, _ := status.FromError(err); st.Code() != tc.errCode {
				t.Errorf("checker.CheckToken(ctx, token, tokenInfo %s)=_, %v, want err code %d", tc.tokenInfo.Email, err, tc.errCode)
			}
		}
	}

	testCheck()

	t.Logf("allow new contributor")
	g := config.Groups[len(config.Groups)-1]
	g.Emails = append(g.Emails, "contributor@gmail.com")

	err = checker.Set(ctx, config)
	if err != nil {
		t.Errorf("checker.Set(ctx, config)=%v; want nil-error", err)
	}
	testCases["contributor2"].errCode = codes.OK

	testCheck()

	t.Logf("remove contributor")
	config.Groups = config.Groups[:len(config.Groups)-1]

	err = checker.Set(ctx, config)
	if err != nil {
		t.Errorf("checker.Set(ctx, config)=%v; want nil-error", err)
	}
	testCases["contributor"].errCode = codes.PermissionDenied
	testCases["contributor2"].errCode = codes.PermissionDenied

	testCheck()

	t.Logf("reject bad googler")
	config.Groups = append([]*pb.Group{
		{
			Id:       "bad-googler",
			Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			Emails:   []string{"malicious@google.com"},
			Reject:   true,
		},
	}, config.Groups...)

	err = checker.Set(ctx, config)
	if err != nil {
		t.Errorf("checker.Set(ctx, config)=%v; want nil-error", err)
	}
	testCases["malicious-googler"].errCode = codes.PermissionDenied

	testCheck()
}

type fakeAuthDB struct {
	db map[string]bool
}

func (f fakeAuthDB) IsMember(ctx context.Context, email, group string) bool {
	return f.db[email+":"+group]
}

func TestCheckGroup(t *testing.T) {
	ctx := context.Background()

	authDB := fakeAuthDB{
		db: map[string]bool{
			"someone@google.com:googler": true,
		},
	}

	for _, tc := range []struct {
		desc      string
		tokenInfo *auth.TokenInfo
		g         *pb.Group
		want      bool
	}{
		{
			desc: "empty group",
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
			g: &pb.Group{
				Id: "empty",
			},
		},
		{
			desc: "email match",
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
			g: &pb.Group{
				Id: "someone-in-google",
				Emails: []string{
					"someone@google.com",
				},
			},
			want: true,
		},
		{
			desc: "domain match",
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
			g: &pb.Group{
				Id: "googler",
				Domains: []string{
					"google.com",
				},
			},
			want: true,
		},
		{
			desc: "authdb match",
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
			g: &pb.Group{
				Id: "googler",
			},
			want: true,
		},
		{
			desc: "email match with audience check",
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
			g: &pb.Group{
				Id:       "someone-in-google",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
				Emails: []string{
					"someone@google.com",
				},
			},
			want: true,
		},
		{
			desc: "domain match with audience check",
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
			g: &pb.Group{
				Id:       "googler",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
				Domains: []string{
					"google.com",
				},
			},
			want: true,
		},
		{
			desc: "authdb match with audience check",
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
			g: &pb.Group{
				Id:       "googler",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
			want: true,
		},
		{
			desc: "email match but audience mismatch",
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "123456-xxxxxx.apps.googleusercontent.com",
			},
			g: &pb.Group{
				Id:       "someone-in-google",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
				Emails: []string{
					"someone@google.com",
				},
			},
		},
		{
			desc: "domain match but audience mismatch",
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "123456-xxxxxx.apps.googleusercontent.com",
			},
			g: &pb.Group{
				Id:       "googler",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
				Domains: []string{
					"google.com",
				},
			},
		},
		{
			desc: "authdb match but audience mismatch",
			tokenInfo: &auth.TokenInfo{
				Email:    "someone@google.com",
				Audience: "123456-xxxxxx.apps.googleusercontent.com",
			},
			g: &pb.Group{
				Id:       "googler",
				Audience: "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com",
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got := checkGroup(ctx, tc.tokenInfo, tc.g, authDB)
			if got != tc.want {
				t.Errorf("checkGroup(ctx, tokenInfo, group, authDB)=%t; want=%t", got, tc.want)
			}
		})
	}
}
