// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/*
Package enduser manages end user information with context.

*/
package enduser

import (
	"context"
	"fmt"
	"reflect"

	"golang.org/x/oauth2"
	"google.golang.org/grpc/metadata"
)

// EmailString holds email string.  It will not output empty string in format.
// Don't use this type in unexported field.  fmt won't invoke formatting method
// on unexported fields. see https://golang.org/pkg/fmt/.
type EmailString string

func (e EmailString) Formatter(fmt.State, rune) {}
func (e EmailString) GoString() string          { return "" }
func (e EmailString) String() string            { return "" }

// EndUser represents end user of httprpc calls.
type EndUser struct {
	Email EmailString
	Group string
	token *oauth2.Token
}

type key int

var userKey key = 0

const (
	// metadata key: allowed 0-9 a-z -_.
	// upper case converted to lower case on wire.
	// we should use lowercase only here to use it as md[key].
	// https://godoc.org/google.golang.org/grpc/metadata#Pairs
	//
	// endpoint proxy will not pass some metadata such as
	// "goma.auth.enduser.email".
	// confirmed "x-[-a-z0-9]+" will go through endpoint proxy.
	emailKey       = "x-goma-enduser-email"
	groupKey       = "x-goma-enduser-group"
	accessTokenKey = "x-goma-enduser-accesstoken"
	tokenTypeKey   = "x-goma-enduser-tokentype"
)

// New creates new EndUser from email, group and oauth2 access token.
func New(email, group string, token *oauth2.Token) *EndUser {
	user := &EndUser{
		Email: EmailString(email),
		Group: group,
		token: token,
	}
	return user
}

// NewContext returns a new Context that carries value u in metadata.
func NewContext(ctx context.Context, u *EndUser) context.Context {
	return context.WithValue(metadata.AppendToOutgoingContext(ctx,
		emailKey, string(u.Email),
		groupKey, u.Group,
		accessTokenKey, u.Token().AccessToken,
		tokenTypeKey, u.Token().TokenType),
		userKey, u)
}

// FromContext returns the EndUser value stored in ctx, if any.
func FromContext(ctx context.Context) (*EndUser, bool) {
	u, ok := ctx.Value(userKey).(*EndUser)
	if ok {
		return u, ok
	}
	u = &EndUser{
		token: &oauth2.Token{},
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return u, ok
	}
	v := md[emailKey]
	if len(v) > 0 {
		u.Email = EmailString(v[0])
	}
	v = md[groupKey]
	if len(v) > 0 {
		u.Group = v[0]
	}
	v = md[accessTokenKey]
	if len(v) > 0 {
		u.token.AccessToken = v[0]
	}
	v = md[tokenTypeKey]
	if len(v) > 0 {
		u.token.TokenType = v[0]
	}
	ok = !reflect.DeepEqual(u, &EndUser{
		token: &oauth2.Token{},
	})
	return u, ok
}

// Token returns end user's access token.
func (u *EndUser) Token() *oauth2.Token {
	if u == nil || u.token == nil {
		return &oauth2.Token{}
	}
	return u.token
}
