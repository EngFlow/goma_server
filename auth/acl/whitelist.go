// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package acl

import (
	"context"

	pb "go.chromium.org/goma/server/proto/auth"
)

const (
	// new goma client client_id
	// https://chromium.googlesource.com/infra/goma/client/+/70685d6cbb19c108d8abf2235edd2d02bed8dded/client/oauth2.cc#72
	GomaClientClientID = "687418631491-r6m1c3pr0lth5atp4ie07f03ae8omefc.apps.googleusercontent.com"
)

// DefaultWhitelist is a loader to provide default whitelist, which pass through EUC.
type DefaultWhitelist struct{}

func (DefaultWhitelist) Load(ctx context.Context) (*pb.ACL, error) {
	return &pb.ACL{
		Groups: []*pb.Group{
			{
				Id:          "chrome-bot",
				Description: "chromium buildbot service account",
				Emails:      []string{"goma-client@chrome-infra-auth.iam.gserviceaccount.com"},
			},
			{
				Id:          "chromium-swarm-dev",
				Description: "staging chromium-swarm-dev bots. http://b/63818232 http://crbug.com/684735",
				Emails:      []string{"pool-chrome@chromium-swarm-dev.iam.gserviceaccount.com"},
			},
			{
				Id:       "googler",
				Audience: GomaClientClientID,
				Domains:  []string{"google.com"},
			},
		},
	}, nil
}
