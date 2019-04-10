// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package descriptor

import (
	"bufio"
	"bytes"
	"debug/elf"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// removeAbsPaths removes paths that start with '/'.
// order will be kept.
func removeAbsPaths(rpaths []string) []string {
	var newRpaths []string
	for _, rpath := range rpaths {
		if strings.HasPrefix(rpath, "/") {
			continue
		}
		newRpaths = append(newRpaths, rpath)
	}

	return newRpaths
}

// relocateLibraries reads an elf binary to list dependent libraries.
// If a dependent library exists in rpath or runpath, it's considered as
// relocatable. However, rpath or runpath is in absolute form, the dependent
// library is considered as unrelocatable.
func relocatableLibraries(fname string) ([]string, error) {
	f, err := elf.Open(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tokens := map[string]string{
		"ORIGIN":   filepath.Dir(fname),
		"LIB":      libname(f),
		"PLATFORM": platform(),
	}
	rpaths, err := f.DynString(elf.DT_RPATH)
	if err != nil {
		return nil, err
	}
	runpaths, err := f.DynString(elf.DT_RUNPATH)
	if err != nil {
		return nil, err
	}
	rpaths = append(rpaths, runpaths...)

	// When rpath is absolute path, it's not relocatable, so skip it.
	// Since $ORIGIN can be absolute, these should be filtered
	// before expanding tokens.
	rpaths = removeAbsPaths(rpaths)
	rpaths = expandRpaths(tokens, rpaths...)

	libs, err := f.ImportedLibraries()
	if err != nil {
		return nil, err
	}
	var libpaths []string
	for _, lib := range libs {
		path, err := lookpath(lib, rpaths)
		if err != nil {
			continue
		}
		libpaths = append(libpaths, path)
	}
	return libpaths, nil
}

func libname(f *elf.File) string {
	if f.FileHeader.Class == elf.ELFCLASS64 {
		return "lib64"
	}
	return "lib"
}

var (
	platformOnce sync.Once
	platformName string
)

func platform() string {
	platformOnce.Do(func() {
		cmd := exec.Command("sleep", "0")
		cmd.Env = []string{"LD_SHOW_AUXV=1"}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return
		}
		s := bufio.NewScanner(bytes.NewReader(out))
		for s.Scan() {
			cols := strings.Fields(s.Text())
			if len(cols) != 2 {
				continue
			}
			if cols[0] == "AT_PLATFORM:" {
				platformName = cols[1]
				break
			}
		}
	})
	return platformName
}

func expandRpaths(tokens map[string]string, rpaths ...string) []string {
	var paths []string
	for _, rpath := range rpaths {
		for _, rpath := range strings.Split(rpath, ":") {
			rpath = expand(tokens, rpath)
			paths = append(paths, rpath)
		}
	}
	return paths
}

func expand(tokens map[string]string, rpath string) string {
	for tok, val := range tokens {
		rpath = strings.Replace(rpath, fmt.Sprintf("$%s", tok), val, -1)
		rpath = strings.Replace(rpath, fmt.Sprintf("${%s}", tok), val, -1)
	}
	return rpath
}
