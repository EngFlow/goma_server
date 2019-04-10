// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package normalizer

import (
	"reflect"
	"testing"

	"github.com/golang/protobuf/proto"

	cmdpb "go.chromium.org/goma/server/proto/command"
)

func TestParseTarget(t *testing.T) {
	for _, tc := range []struct {
		input     string
		want      target
		wantError bool
	}{
		{
			input: "arm-eabi",
			want: target{
				arch:     "arm",
				archType: "arm",
				env:      "eabi",
			},
		},
		{
			input: "arm-linux-androideabi",
			want: target{
				arch:     "arm",
				archType: "arm",
				os:       "linux",
				env:      "androideabi",
			},
		},
		{
			input: "arm-none-eabi",
			want: target{
				arch:     "arm",
				archType: "arm",
				os:       "none",
				env:      "eabi",
			},
		},
		{
			input: "armv7a-cros-linux-gnueabi",
			want: target{
				arch:     "armv7a",
				archType: "armv7a",
				vendor:   "cros",
				os:       "linux",
				env:      "gnueabi",
			},
		},
		{
			input: "i486-linux-gnu",
			want: target{
				arch:     "i486",
				archType: "i686",
				os:       "linux",
				env:      "gnu",
			},
		},
		{
			input: "i686-android-linux",
			want: target{
				arch:     "i686",
				archType: "i686",
				vendor:   "android",
				os:       "linux",
			},
		},
		{
			input: "i686-apple-darwin11",
			want: target{
				arch:     "i686",
				archType: "i686",
				vendor:   "apple",
				os:       "darwin11",
			},
		},
		{
			input: "i686-linux",
			want: target{
				arch:     "i686",
				archType: "i686",
				os:       "linux",
			},
		},
		{
			input: "i686-linux-android",
			want: target{
				arch:     "i686",
				archType: "i686",
				os:       "linux",
				env:      "android",
			},
		},
		{
			input: "i686-pc-linux-gnu",
			want: target{
				arch:     "i686",
				archType: "i686",
				vendor:   "pc",
				os:       "linux",
				env:      "gnu",
			},
		},
		{
			input: "i686-unknown-linux-gnu",
			want: target{
				arch:     "i686",
				archType: "i686",
				vendor:   "unknown",
				os:       "linux",
				env:      "gnu",
			},
		},
		{
			input: "mipsel-linux-android",
			want: target{
				arch:     "mipsel",
				archType: "mipsel",
				os:       "linux",
				env:      "android",
			},
		},
		{
			input: "mipsel-linux-uclibc",
			want: target{
				arch:     "mipsel",
				archType: "mipsel",
				os:       "linux",
				env:      "uclibc",
			},
		},
		{
			input: "sh-linux-gnu",
			want: target{
				arch:     "sh",
				archType: "sh",
				os:       "linux",
				env:      "gnu",
			},
		},
		{
			input: "x86_64-apple-darwin10.6.0",
			want: target{
				arch:     "x86_64",
				archType: "x86_64",
				vendor:   "apple",
				os:       "darwin10.6.0",
			},
		},
		{
			input: "x86_64-apple-darwin16.5.0",
			want: target{
				arch:     "x86_64",
				archType: "x86_64",
				vendor:   "apple",
				os:       "darwin16.5.0",
			},
		},
		{
			input: "x86_64-cros-linux-gnu",
			want: target{
				arch:     "x86_64",
				archType: "x86_64",
				vendor:   "cros",
				os:       "linux",
				env:      "gnu",
			},
		},
		{
			input: "x86_64-linux",
			want: target{
				arch:     "x86_64",
				archType: "x86_64",
				os:       "linux",
			},
		},
		{
			input: "x86_64-linux-gnu",
			want: target{
				arch:     "x86_64",
				archType: "x86_64",
				os:       "linux",
				env:      "gnu",
			},
		},
		{
			input: "x86_64-nacl",
			want: target{
				arch:     "x86_64",
				archType: "x86_64",
				os:       "nacl",
			},
		},
		{
			input: "x86_64-pc-linux-gnu",
			want: target{
				arch:     "x86_64",
				archType: "x86_64",
				vendor:   "pc",
				os:       "linux",
				env:      "gnu",
			},
		},
		{
			input: "x86_64-unknown-linux-gnu",
			want: target{
				arch:     "x86_64",
				archType: "x86_64",
				vendor:   "unknown",
				os:       "linux",
				env:      "gnu",
			},
		},
		{
			input: "i386-pc-windows-msvc",
			want: target{
				arch:     "i386",
				archType: "i686",
				vendor:   "pc",
				os:       "windows",
				env:      "msvc",
			},
		},
		{
			input: "x86_64-pc-windows-msvc",
			want: target{
				arch:     "x86_64",
				archType: "x86_64",
				vendor:   "pc",
				os:       "windows",
				env:      "msvc",
			},
		},
		// error cases
		{
			input:     "x86_64",
			wantError: true,
		},
		{
			input:     "x86_64-too-long-target-name-but-linux-msvc",
			wantError: true,
		},
		{
			input:     "x86_64-dummy-long-target",
			wantError: true,
		},
	} {
		result, err := parseTarget(tc.input)
		if tc.wantError && err == nil {
			t.Errorf("parseTarget(%q)=_,nil; want err", tc.input)
		}
		if !tc.wantError && err != nil {
			t.Errorf("parseTarget(%q)=_,%v; want nil", tc.input, err)
		}
		if err == nil && !reflect.DeepEqual(result, tc.want) {
			t.Errorf("parseTarget(%q)=%q; want %q", tc.input, result, tc.want)
		}
	}
}

func TestNormalizedTargetString(t *testing.T) {
	for _, tc := range []struct {
		input target
		want  string
	}{
		{
			input: target{
				arch:     "x86_64",
				archType: "x86_64",
				vendor:   "pc",
				os:       "linux",
				env:      "gnu",
			},
			want: "x86_64-linux",
		},
		{
			input: target{
				arch:     "x86_64",
				archType: "x86_64",
				vendor:   "cros",
				os:       "linux",
				env:      "gnu",
			},
			want: "x86_64-cros-linux",
		},
		{
			input: target{
				arch:     "i486",
				archType: "i686",
				vendor:   "pc",
				os:       "windows",
				env:      "msvc",
			},
			want: "i686-windows-msvc",
		},
		{
			input: target{
				arch:     "x86_64",
				archType: "x86_64",
				vendor:   "apple",
				os:       "darwin16.5.0",
			},
			want: "x86_64-darwin",
		},
	} {
		actual := normalizedTargetString(tc.input)
		if actual != tc.want {
			t.Errorf("normalizedTargetString(%q)=%q; want %q", tc.input, actual, tc.want)
		}
	}
}

func TestSelector(t *testing.T) {
	for _, tc := range []struct {
		input     cmdpb.Selector
		want      cmdpb.Selector
		wantError bool
	}{
		{
			input: cmdpb.Selector{
				Name:       "clang",
				Version:    "4.2.1[clang version 5.0.0 (trunk 300839)]",
				Target:     "x86_64-unknown-linux-gnu",
				BinaryHash: "5f650cc98121b383aaa25e53a135d8b4c5e0748f25082b4f2d428a5934d22fda",
			},
			want: cmdpb.Selector{
				Name:       "clang",
				Version:    "4.2.1[clang version 5.0.0 (trunk 300839)]",
				Target:     "x86_64-linux",
				BinaryHash: "5f650cc98121b383aaa25e53a135d8b4c5e0748f25082b4f2d428a5934d22fda",
			},
		},
		{
			input: cmdpb.Selector{
				Name:       "clang",
				Version:    "4.2.1[clang version 5.0.0 (trunk 300839)]",
				BinaryHash: "5f650cc98121b383aaa25e53a135d8b4c5e0748f25082b4f2d428a5934d22fda",
			},
			want: cmdpb.Selector{
				Name:       "clang",
				Version:    "4.2.1[clang version 5.0.0 (trunk 300839)]",
				BinaryHash: "5f650cc98121b383aaa25e53a135d8b4c5e0748f25082b4f2d428a5934d22fda",
			},
		},
		{
			input: cmdpb.Selector{
				Name:       "javac",
				Target:     "java",
				Version:    "1.8.0_45-internal",
				BinaryHash: "609aeefbab4b988d1a3705a3da442590c6f22aa8f27036f8a08deaabd3714c27",
			},
			want: cmdpb.Selector{
				Name:       "javac",
				Target:     "java",
				Version:    "1.8.0_45-internal",
				BinaryHash: "609aeefbab4b988d1a3705a3da442590c6f22aa8f27036f8a08deaabd3714c27",
			},
		},
		{
			input: cmdpb.Selector{
				Name:       "cl.exe",
				Target:     "x64",
				Version:    "19.11.25505",
				BinaryHash: "5d734edd36be5be66be72f543522e95368a88da687467b5797137e47cbdeecd0",
			},
			want: cmdpb.Selector{
				Name:       "cl.exe",
				Target:     "x64",
				Version:    "19.11.25505",
				BinaryHash: "5d734edd36be5be66be72f543522e95368a88da687467b5797137e47cbdeecd0",
			},
		},
		{
			input: cmdpb.Selector{
				Name:       "clang",
				Version:    "4.2.1[clang version 5.0.0 (trunk 300839)]",
				Target:     "x86_64-unknown-linux-gnu-should-parse-error",
				BinaryHash: "5f650cc98121b383aaa25e53a135d8b4c5e0748f25082b4f2d428a5934d22fda",
			},
			wantError: true,
		},
	} {
		actual, err := Selector(tc.input)
		if err != nil && !tc.wantError {
			t.Errorf("Selector(%q)=_,%v; want nil", tc.input, err)
		}
		if err == nil && tc.wantError {
			t.Errorf("Selector(%q)=_,nil; want err", tc.input)
		}
		if err == nil && !proto.Equal(&actual, &tc.want) {
			t.Errorf("Selector(%q)=%q; want %q", tc.input, actual, tc.want)
		}
	}
}

func TestTarget(t *testing.T) {
	for _, tc := range []struct {
		input     string
		want      string
		wantError bool
	}{
		{
			input: "x86_64-unknown-linux-gnu",
			want:  "x86_64-linux",
		},
		{
			input: "x86_64-apple-darwin16.5.0",
			want:  "x86_64-darwin",
		},
		{
			input: "x86_64--nacl",
			want:  "x86_64-nacl",
		},
		{
			input: "x86_64--darwin",
			want:  "x86_64-darwin",
		},
		{
			input:     "x86_64-unknown-linux-gnu-should-parse-error",
			wantError: true,
		},
	} {
		actual, err := Target(tc.input)
		if err != nil && !tc.wantError {
			t.Errorf("Target(%s)=_,%v; want nil", tc.input, err)
		}
		if err == nil && tc.wantError {
			t.Errorf("Target(%s)=_,nil; want err", tc.input)
		}
		if err == nil && actual != tc.want {
			t.Errorf("Target(%s)=%s; want %s", tc.input, actual, tc.want)
		}
	}
}
