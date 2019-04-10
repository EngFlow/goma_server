// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package enduser

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/oauth2"
	"google.golang.org/grpc/metadata"
)

func TestEmailString(t *testing.T) {
	v := EmailString("someone@google.com")
	if got := fmt.Sprint(v); strings.Contains(got, "someone@google.com") {
		t.Errorf("fmt.Sprint(v)=%s; leak email address", got)
	}
	if got := fmt.Sprintf("%[1]s %[1]v %+[1]v %#[1]v %[1]q", v); strings.Contains(got, "someone@google.com") {
		t.Errorf(`fmt.Sprintf("...", v)=%s; leak email address`, got)
	}

	sv := struct {
		Email EmailString
	}{
		Email: v,
	}
	if got := fmt.Sprint(sv); strings.Contains(got, "someone@google.com") {
		t.Errorf("fmt.Sprint(sv)=%s; leak email address", got)
	}
	if got := fmt.Sprintf("%[1]s %[1]v %+[1]v %#[1]v %[1]q", sv); strings.Contains(got, "someone@google.com") {
		t.Errorf(`fmt.Sprintf("...", sv)=%s; leak email address`, got)
	}

}

func TestNewContext(t *testing.T) {
	ctx := context.Background()
	u := New("someone@google.com", "googler", &oauth2.Token{
		AccessToken: "token-value",
		TokenType:   "Bearer",
	})

	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-api-key", "xxx"))
	ctx = NewContext(ctx, u)

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("NewContext failed to set md")
	}
	wantMD := metadata.Pairs(
		"x-api-key", "xxx",
		emailKey, "someone@google.com",
		groupKey, "googler",
		accessTokenKey, "token-value",
		tokenTypeKey, "Bearer")
	if !reflect.DeepEqual(md, wantMD) {
		t.Errorf("NewContext set metadata %#v; want %#v", md, wantMD)
	}
}

func TestFromContextEmpty(t *testing.T) {
	ctx := context.Background()

	t.Logf("empty context")
	_, ok := FromContext(ctx)
	if ok {
		t.Errorf("FromContext(ctx)=_, %t; want=_, false", ok)
	}
}

func TestFromContext(t *testing.T) {
	ctx := context.Background()
	u := New("someone@google.com", "googler", &oauth2.Token{
		AccessToken: "token-value",
		TokenType:   "Bearer",
	})
	ctx = NewContext(ctx, u)

	got, ok := FromContext(ctx)
	if !ok {
		t.Errorf("FromContext(ctx)=_, %t; want=_, true", ok)
	}
	if !reflect.DeepEqual(got, u) {
		t.Errorf("FromContext(ctx)=%#v; want=%#v", got, u)
	}
}

func TestFromContextFromMetadata(t *testing.T) {
	ctx := context.Background()
	md := metadata.Pairs(
		emailKey, "someone@google.com",
		groupKey, "googler",
		accessTokenKey, "token-value",
		tokenTypeKey, "Bearer")
	ctx = metadata.NewIncomingContext(ctx, md)

	got, ok := FromContext(ctx)
	if !ok {
		t.Errorf("FromContext(ctx)=_, %t; want=_, true", ok)
	}
	u := New("someone@google.com", "googler", &oauth2.Token{
		AccessToken: "token-value",
		TokenType:   "Bearer",
	})
	if !reflect.DeepEqual(got, u) {
		t.Errorf("FromContext(ctx)=%#v; want=%#v", got, u)
	}
}

func TestFromContextFromOtherMetadata(t *testing.T) {
	ctx := context.Background()
	md := metadata.Pairs("x-api-key", "xxx")
	ctx = metadata.NewIncomingContext(ctx, md)

	got, ok := FromContext(ctx)
	if ok {
		t.Errorf("FromContext(ctx)=%#v, %t; want=_, false", got, ok)
	}
}
