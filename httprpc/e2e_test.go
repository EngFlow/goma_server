// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package httprpc_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/httprpc"

	pb "go.chromium.org/goma/server/proto/settings"
)

func TestHandlerAndClient(t *testing.T) {
	for _, contentEncoding := range []string{"identity", "deflate", "gzip"} {
		t.Run(contentEncoding, func(t *testing.T) {
			resp := &pb.SettingsResp{
				Settings: &pb.Settings{
					Name: "test",
				},
			}
			req := &pb.SettingsReq{
				UseCase: "test",
			}

			h := httprpc.Handler("test", &pb.SettingsReq{}, &pb.SettingsResp{},
				func(ctx context.Context, r proto.Message) (proto.Message, error) {
					if !proto.Equal(r, req) {
						t.Errorf("handler req=%#v; want=%#v", r, req)
					}
					return resp, nil
				})

			mux := http.NewServeMux()
			mux.Handle("/settings", h)
			s := httptest.NewTLSServer(mux)
			defer s.Close()

			ctx := context.Background()
			gotResp := &pb.SettingsResp{}

			client := &httprpc.Client{
				Client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{
							InsecureSkipVerify: true,
						},
					},
				},
				URL:             s.URL + "/settings",
				ContentEncoding: contentEncoding,
			}

			err := client.Call(ctx, req, gotResp)
			if err != nil {
				t.Errorf("httprpc.Call=%v; want nil err", err)
			}
			if !proto.Equal(gotResp, resp) {
				t.Errorf("httprpc.Call=%#v; want=%#v", gotResp, resp)
			}
			if client.ContentEncoding != contentEncoding {
				t.Errorf("client.ContentEncoding=%q; want=%q", client.ContentEncoding, contentEncoding)
			}
		})
	}
}
