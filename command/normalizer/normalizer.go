// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package normalizer

import (
	"fmt"
	"strings"

	cmdpb "go.chromium.org/goma/server/proto/command"
)

type target struct {
	arch     string
	archType string
	vendor   string
	os       string
	env      string
}

func parseTarget(t string) (target, error) {
	tokens := strings.Split(t, "-")
	if len(tokens) < 2 || len(tokens) > 4 {
		return target{}, fmt.Errorf("target has invalid token number: len=%d: t=%s", len(tokens), t)
	}

	var arch, archType, vendor, os, env string
	arch = tokens[0]
	switch {
	case len(arch) == 4 && arch[0] == 'i' && arch[1] >= '0' && arch[1] <= '9' && arch[2:] == "86":
		archType = "i686"
	case arch == "amd64":
		archType = "x86_64"
	default:
		archType = arch
	}
	i := len(tokens) - 1
	switch tokens[i] {
	case "eabi", "gnu", "gnueabi", "gnueabihf", "macho", "android", "androideabi", "uclibc", "msvc":
		env = tokens[i]
		i--
	}
	switch i {
	case 3:
		return target{}, fmt.Errorf("still have 4 tokens: %s", t)
	case 2:
		vendor = tokens[1]
		os = tokens[2]
	case 1:
		os = tokens[1]
	case 0:
	default:
		return target{}, fmt.Errorf("index is broken: %q index=%d", t, i)
	}
	return target{
		arch:     arch,
		archType: archType,
		vendor:   vendor,
		os:       os,
		env:      env}, nil
}

func normalizedTargetString(t target) string {
	ret := []string{t.archType}
	// We'll only take care of vendor name if it is "cros" to avoid
	// ambiguity on ChromeOS compilers (b/17324429).
	// Otherwise, we ignore vendor names as it doesn't affect so much for
	// the features of compilers.
	if t.vendor == "cros" {
		ret = append(ret, t.vendor)
	}

	if t.vendor == "apple" && strings.HasPrefix(t.os, "darwin") {
		ret = append(ret, "darwin")
	} else if t.os != "" {
		ret = append(ret, t.os)
	}

	if t.env != "" && t.env != "gnu" {
		ret = append(ret, t.env)
	}
	return strings.Join(ret, "-")
}

// Target converts a target to a normalized one.
func Target(t string) (string, error) {
	p, err := parseTarget(t)
	if err != nil {
		return "", err
	}
	return normalizedTargetString(p), nil
}

// Selector returns a selector whose target is normalized.
func Selector(s cmdpb.Selector) (cmdpb.Selector, error) {
	if s.Target == "" || s.Target == "java" {
		return s, nil
	}
	// For MSVC, we don't have to normalize target.
	if s.Name == "cl.exe" {
		return s, nil
	}

	t, err := Target(s.GetTarget())
	if err != nil {
		return cmdpb.Selector{}, err
	}
	s.Target = t
	return s, nil
}
