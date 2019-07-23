// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"sort"
	"strings"

	"github.com/golang/protobuf/proto"

	gomapb "go.chromium.org/goma/server/proto/api"
	nsjailpb "go.chromium.org/goma/server/proto/nsjail"
)

const (
	nsjailRunWrapperScript = `#!/bin/bash
set -e

if [[ "$WORK_DIR" == "" ]]; then
  echo "ERROR: WORK_DIR is not set" >&2
  exit 1
fi

rundir="$(pwd)"
chroot_workdir="/tmp/goma_chroot"

#
# mount directories under $chroot_workdir and execute.
#
run_dirs=($(ls -1 "$rundir"))
sys_dirs=(dev proc)

# RBE server generates __action_home__XXXXXXXXXX directory in $rundir
# (note: XXXXXXXXXX is a random).  Let's skip it because we do not use that.
# mount directories in the request.
for d in "${run_dirs[@]}"; do
  if [[ "$d" == __action_home__* ]]; then
    continue
  fi
  mkdir -p "$chroot_workdir/$d"
  mount --bind "$rundir/$d" "$chroot_workdir/$d"
done

# mount directories not included in the request.
for d in "${sys_dirs[@]}"; do
  # avoid to mount system directories if that exist in the user's request.
  if [[ -d "$rundir/$d" ]]; then
    continue
  fi
  # directory will be mounted by nsjail later.
  mkdir -p "$chroot_workdir/$d"
done
# needed to make nsjail bind device files.
touch "$chroot_workdir/dev/urandom"
touch "$chroot_workdir/dev/null"

# currently running with root. run the command with nobody:nogroup with chroot.
# We use nsjail to chdir without running bash script inside chroot, and
# libc inside chroot can be different from libc outside.
nsjail --quiet --config "$WORK_DIR/nsjail.cfg" -- "$@"
`
)

// pathFromToolchainSpec returns ':'-joined directories of paths in toolchain spec.
// Since symlinks may point to executables, having directories with executables
// may not work, but it is a bit cumbersome to analyze symlinks.
// Also, having library directories in PATH should be harmless because
// the Goma client may not include multiple subprograms with the same name.
func pathFromToolchainSpec(cfp clientFilePath, ts []*gomapb.ToolchainSpec) string {
	m := make(map[string]bool)
	for _, e := range ts {
		m[cfp.Dir(e.GetPath())] = true
	}
	var r []string
	for k := range m {
		if k == "" || k == "." {
			continue
		}
		r = append(r, k)
	}
	// This function must return the same result for the same input, but go
	// does not guarantee the iteration order.
	sort.Strings(r)
	return strings.Join(r, ":")
}

// nsjailConfig returns nsjail configuration.
// When you modify followings, please make sure it matches
// nsjailRunWrapperScript above.
func nsjailConfig(cwd string, cfp clientFilePath, ts []*gomapb.ToolchainSpec, envs []string) []byte {
	chrootWorkdir := "/tmp/goma_chroot"
	cfg := &nsjailpb.NsJailConfig{
		Uidmap: []*nsjailpb.IdMap{
			{
				InsideId:  proto.String("nobody"),
				OutsideId: proto.String("nobody"),
			},
		},
		Gidmap: []*nsjailpb.IdMap{
			{
				InsideId:  proto.String("nogroup"),
				OutsideId: proto.String("nogroup"),
			},
		},
		Mount: []*nsjailpb.MountPt{
			{
				Src:    proto.String(chrootWorkdir),
				Dst:    proto.String("/"),
				IsBind: proto.Bool(true),
				Rw:     proto.Bool(true),
				IsDir:  proto.Bool(true),
			},
			{
				Src:    proto.String("/dev/null"),
				Dst:    proto.String("/dev/null"),
				Rw:     proto.Bool(true),
				IsBind: proto.Bool(true),
			},
			{
				Src:    proto.String("/dev/urandom"),
				Dst:    proto.String("/dev/urandom"),
				IsBind: proto.Bool(true),
			},
		},
		Cwd: proto.String(cwd),
		// TODO: use log file and print to server log.
		LogLevel:  nsjailpb.LogLevel_WARNING.Enum(),
		MountProc: proto.Bool(true),
		Envar: append(
			[]string{
				"PATH=" + pathFromToolchainSpec(cfp, ts),
				// Dummy home directory is needed by pnacl-clang to
				// import site.py to import user-defined python
				// packages.
				"HOME=/",
			},
			// Add client-side environemnt to execution environment.
			envs...),
		RlimitAsType:    nsjailpb.RLimit_INF.Enum(),
		RlimitFsizeType: nsjailpb.RLimit_INF.Enum(),
		// TODO: relax RLimit from the default.
		// Default size might be too strict, and not suitable for
		// compiling.
	}
	return []byte(proto.MarshalTextString(cfg))
}
