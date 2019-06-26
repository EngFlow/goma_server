// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"github.com/golang/protobuf/proto"

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
# needed to make nsjail bind /dev/urandom.
touch "$chroot_workdir/dev/urandom"

# currently running with root. run the command with nobody:nogroup with chroot.
# We use nsjail to chdir without running bash script inside chroot, and
# libc inside chroot can be different from libc outside.
nsjail --quiet --config "$WORK_DIR/nsjail.cfg" -- "$@"
`
)

// nsjailConfig returns nsjail configuration.
// When you modify followings, please make sure it matches
// nsjailRunWrapperScript above.
func nsjailConfig(cwd string) []byte {
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
				Src:    proto.String("/dev/urandom"),
				Dst:    proto.String("/dev/urandom"),
				IsBind: proto.Bool(true),
			},
		},
		Cwd: proto.String(cwd),
		// TODO: use log file and print to server log.
		LogLevel:  nsjailpb.LogLevel_WARNING.Enum(),
		MountProc: proto.Bool(true),
		Envar: []string{
			// HACK: ChromeOS clang wrapper needs this.
			// https://chromium.googlesource.com/chromiumos/overlays/chromiumos-overlay/+/f15eab2d792acfe1ee5eca4d95792c081558cf84/sys-devel/gcc/files/sysroot_wrapper.body#69
			// TODO: set better path upon needs.
			// e.g. auto generate from where executables and symlink
			// to executable exists, or choose directories with
			// executables from PATH sent by clients.
			"PATH=",
		},
		RlimitAsType:    nsjailpb.RLimit_INF.Enum(),
		RlimitFsizeType: nsjailpb.RLimit_INF.Enum(),
		// TODO: relax RLimit from the default.
		// Default size might be too strict, and not suitable for
		// compiling.
	}
	return []byte(proto.MarshalTextString(cfg))
}
