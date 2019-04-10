// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"
	"net/url"
	"testing"
)

type dummyBackend struct {
	Backend
	id string
}

func TestMixerSelectBackend(t *testing.T) {
	ctx := context.Background()
	winQuery, err := url.ParseQuery("win")
	if err != nil {
		t.Fatalf("win: %v", err)
	}
	stagingQuery, err := url.ParseQuery("staging")
	if err != nil {
		t.Fatalf("staging: %v", err)
	}
	stagingWinQuery, err := url.ParseQuery("win&staging")
	if err != nil {
		t.Fatalf("win&staging: %v", err)
	}
	mixer := Mixer{
		backends: map[string]Backend{
			backendKey("goma-group1", nil): dummyBackend{
				id: "group1-backend",
			},
			backendKey("goma-group2", nil): dummyBackend{
				id: "group2-backend",
			},
			backendKey("service-account-goma-client", nil): dummyBackend{
				id: "prod-backend",
			},
			backendKey("service-account-goma-client", winQuery): dummyBackend{
				id: "prod-backend",
			},
			backendKey("service-account-goma-client", stagingQuery): dummyBackend{
				id: "staging-backend",
			},
			backendKey("service-account-goma-client", stagingWinQuery): dummyBackend{
				id: "staging-backend",
			},
		},
		defaultBackend: dummyBackend{
			id: "default-backend",
		},
	}

	for _, tc := range []struct {
		group    string
		rawQuery string
		target   string
	}{
		{
			group:  "googlers",
			target: "default-backend",
		},
		{
			group:    "googlers",
			rawQuery: "win",
			target:   "default-backend",
		},
		{
			group:    "googlers",
			rawQuery: "staging",
			target:   "default-backend",
		},
		{
			group:  "goma-group1",
			target: "group1-backend",
		},
		{
			group:    "goma-group1",
			rawQuery: "win",
			target:   "group1-backend",
		},
		{
			group:  "goma-group2",
			target: "group2-backend",
		},
		{
			group:  "service-account-goma-client",
			target: "prod-backend",
		},
		{
			group:    "service-account-goma-client",
			rawQuery: "win",
			target:   "prod-backend",
		},
		{
			group:    "service-account-goma-client",
			rawQuery: "staging",
			target:   "staging-backend",
		},
		{
			group:    "service-account-goma-client",
			rawQuery: "staging&win",
			target:   "staging-backend",
		},
		{
			group:    "service-account-goma-client",
			rawQuery: "win&staging",
			target:   "staging-backend",
		},
		{
			group:    "service-account-goma-client",
			rawQuery: "staging&extra",
			target:   "prod-backend",
		},
	} {
		t.Run(tc.group+"?"+tc.rawQuery, func(t *testing.T) {
			q, err := url.ParseQuery(tc.rawQuery)
			if err != nil {
				t.Fatal(err)
			}
			backend, found := mixer.selectBackend(ctx, tc.group, q)
			if !found {
				t.Fatal("not found")
			}
			if got, want := backend.(dummyBackend).id, tc.target; got != want {
				t.Errorf("selectBackend=%q; want=%q", got, want)
			}
		})
	}
}
