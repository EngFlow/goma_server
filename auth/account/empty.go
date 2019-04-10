// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package account

import "errors"

// Empty doesn't provide any service accounts.
type Empty struct{}

func (e Empty) New(name string) (Account, error) {
	return nil, errors.New("empty has no service account")
}
