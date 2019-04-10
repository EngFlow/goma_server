// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package authdb

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"go.chromium.org/goma/server/httprpc"
	authdbrpc "go.chromium.org/goma/server/httprpc/authdb"
	pb "go.chromium.org/goma/server/proto/auth"
)

type fakeAuthDBServer struct {
	t        *testing.T
	want     *pb.CheckMembershipReq
	resp     *pb.CheckMembershipResp
	respErrs []error
}

func (a *fakeAuthDBServer) CheckMembership(ctx context.Context, req *pb.CheckMembershipReq) (*pb.CheckMembershipResp, error) {
	if len(a.respErrs) > 0 {
		var err error
		err, a.respErrs = a.respErrs[0], a.respErrs[1:]
		return nil, err
	}
	if !proto.Equal(req, a.want) {
		a.t.Errorf("CheckMembership: req=%#v; want=%#v", req, a.want)
		return nil, errors.New("unexpected request")
	}
	return a.resp, nil
}

func TestClient(t *testing.T) {
	ctx := context.Background()
	fakeserver := &fakeAuthDBServer{}
	s := httptest.NewServer(authdbrpc.Handler(fakeserver))
	defer s.Close()

	for _, tc := range []struct {
		desc         string
		email, group string
		resp         bool
		respErrs     []error
		want         bool
	}{
		{
			desc:  "ok",
			email: "someone@google.com",
			group: "goma-googlers",
			resp:  true,
			want:  true,
		},
		{
			desc:  "not member",
			email: "someone@example.com",
			group: "goma-googlers",
			resp:  false,
			want:  false,
		},
		{
			desc:     "temp failure",
			email:    "someone@google.com",
			group:    "goma-googlers",
			resp:     true,
			respErrs: []error{grpc.Errorf(codes.Unavailable, "unavailable")},
			want:     true,
		},
		{
			desc:     "temp failure false",
			email:    "someone@google.com",
			group:    "goma-googlers",
			resp:     false,
			respErrs: []error{grpc.Errorf(codes.Unavailable, "unavailable")},
			want:     false,
		},
		{
			desc:     "server error",
			email:    "someone@google.com",
			group:    "goma-googlers",
			resp:     true,
			respErrs: []error{grpc.Errorf(codes.InvalidArgument, "bad request")},
			want:     false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			// can't start server in each test case,
			// due to duplicate metrics collector registration.
			fakeserver.t = t
			fakeserver.want = &pb.CheckMembershipReq{
				Email: tc.email,
				Group: tc.group,
			}
			fakeserver.resp = &pb.CheckMembershipResp{
				IsMember: tc.resp,
			}
			fakeserver.respErrs = tc.respErrs
			c := Client{
				Client: &httprpc.Client{
					Client: s.Client(),
					URL:    s.URL + "/authdb/checkMembership",
				},
			}
			got := c.IsMember(ctx, tc.email, tc.group)
			if got != tc.want {
				t.Errorf("IsMember(ctx, %q, %q)=%t; want=%t", tc.email, tc.group, got, tc.want)
			}
		})
	}
}
