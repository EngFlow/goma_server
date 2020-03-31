// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"errors"
)

// TODO: share exec/clangcl.go ?

// clangclRelocatableReq checks if the request (args, envs) uses relative
// paths only and doesn't use flags that generates output including cwd,
// so will generate cwd-agnostic outputs
// (files/stdout/stderr will not include cwd dependent paths).
//
// TODO: implement it.
func clangclRelocatableReq(args, envs []string) error {
	return errors.New("no relocatable check for clang-cl")
}

// clangclOutputs returns output files from clang-cl command line.
// https://clang.llvm.org/docs/UsersManual.html#id8
//  /Fo<obj> and /Fd<pdb> is used in Cross-compiling Chrome/win
//  but clang-cl currently doesn't emit pdb.
// https://chromium.googlesource.com/chromium/src/+/lkcr/docs/win_cross.md
// TODO: support output directory (ends in / or \)?
func clangclOutputs(args []string) []string {
	var outputs []string
	outputArg := false

	for _, arg := range args {
		switch {
		case outputArg:
			outputs = append(outputs, arg)
			outputArg = false

		case arg == "/o" || arg == "-o": // /o <file or directory>
			outputArg = true

		case len(arg) > 2 &&
			(arg[0] == '-' || arg[0] == '/') &&
			arg[1] == 'o':
			outputs = append(outputs, arg[2:])

		case len(arg) > 3 &&
			(arg[0] == '-' || arg[0] == '/') &&
			arg[1] == 'F' &&
			(arg[2] == 'o' || // /Fo<obj>
				arg[2] == 'i' || // /Fi<file>  preproc output
				arg[2] == 'a' || // /Fa<file>  asm output
				arg[2] == 'e' || // /Fe<exec>
				arg[2] == 'p'): // /Fp<pch>
			// TODO: /Fd<pdb> if clang-cl emits pdb.
			outputs = append(outputs, arg[3:])
		}
	}
	return outputs
}
