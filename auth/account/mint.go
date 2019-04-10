// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package account manages service account.
package account

import (
	"context"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
)

// Account represents service account.
type Account interface {
	// Equals compare account with other.
	Equals(other Account) bool

	// Token generates new oauth2 token for the account.
	Token(ctx context.Context) (*oauth2.Token, error)
}

// Pool manages service accounts.
type Pool interface {
	// New creates new account for name.
	New(name string) (Account, error)
}

// JSONDir is a Pool with json files.
// It should be used for experiments only.
// we need to rotate keys.
// It uses application default credential, if "default" is requested.
// https://cloud.google.com/docs/authentication/production
type JSONDir struct {
	Dir    string
	Scopes []string
}

type serviceAccount struct {
	name   string
	config *jwt.Config

	mu sync.Mutex
	t  *oauth2.Token
}

type defaultServiceAccount struct {
	scopes []string
	mu     sync.Mutex
	cred   *google.Credentials
	t      *oauth2.Token
}

// New creates new account by loading json file in the dir.
// if name is "default", returns default service account instead.
func (j JSONDir) New(name string) (Account, error) {
	if name == "default" {
		return &defaultServiceAccount{scopes: j.Scopes}, nil
	}
	keyFile := filepath.Join(j.Dir, name+".json")
	jsonKey, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	config, err := google.JWTConfigFromJSON(jsonKey, j.Scopes...)
	if err != nil {
		return nil, err
	}
	return &serviceAccount{
		name:   name,
		config: config,
	}, nil
}

// Equals checks other account has same name and config.
func (sa *serviceAccount) Equals(other Account) bool {
	if other == nil {
		return false
	}
	osa, ok := other.(*serviceAccount)
	if !ok {
		return false
	}
	if sa.name != osa.name {
		return false
	}
	return reflect.DeepEqual(sa.config, osa.config)
}

// Token generates new oauth2 token.
func (sa *serviceAccount) Token(ctx context.Context) (*oauth2.Token, error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	if !sa.t.Valid() {
		var err error
		sa.t, err = sa.config.TokenSource(ctx).Token()
		if err != nil {
			return nil, err
		}
	}
	return sa.t, nil
}

// Equals checks other account has same default service account.
func (sa *defaultServiceAccount) Equals(other Account) bool {
	if other == nil {
		return false
	}
	_, ok := other.(*defaultServiceAccount)
	return ok
}

// Token generates new oauth2 token.
func (sa *defaultServiceAccount) Token(ctx context.Context) (*oauth2.Token, error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	if sa.cred == nil {
		var err error
		sa.cred, err = google.FindDefaultCredentials(ctx, sa.scopes...)
		if err != nil {
			return nil, err
		}
	}
	if !sa.t.Valid() {
		var err error
		sa.t, err = sa.cred.TokenSource.Token()
		if err != nil {
			return nil, err
		}
	}
	return sa.t, nil
}

// TODO: provide another account pool using SignJWT
// TODO: provide another account pool using luci-token-server.
