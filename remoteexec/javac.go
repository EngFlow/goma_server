// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

// TODO: share exec/javac.go ?

// javacOutputDirs returns output directories from javac command line.
func javacOutputDirs(args []string) []string {
	var dirs []string
	dirArg := false

	for _, arg := range args {
		switch {
		case dirArg:
			dirs = append(dirs, arg)
			dirArg = false

		case arg == "-s" || arg == "-d":
			dirArg = true
		}
	}
	return dirs
}
