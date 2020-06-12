// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package acl performs access control with ACL.
package acl

import "context"

// ACL manages access control list.
type ACL struct {
	Loader
	Checker
}

// Update loads acl by Loader and sets it to Checker.
func (a *ACL) Update(ctx context.Context) error {
	if a.Loader == nil {
		a.Loader = DefaultAllowlist{}
	}
	config, err := a.Loader.Load(ctx)
	if err != nil {
		return err
	}
	return a.Checker.Set(ctx, config)
}
