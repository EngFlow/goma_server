// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package descriptor

import (
	"fmt"

	"github.com/golang/protobuf/proto"

	"go.chromium.org/goma/server/command/descriptor/posixpath"
	"go.chromium.org/goma/server/command/descriptor/winpath"
	pb "go.chromium.org/goma/server/proto/command"
)

// FilePath provides client's filepath.
type FilePath interface {
	// IsAbs reports whether the path is absolute.
	IsAbs(path string) bool

	// Base returns the last element of path, typically
	// the filename.
	Base(string) string

	// Dir returns all but the last element of path, typically
	// the path's directory.
	// Different filepath.Dir, it won't clean "..".
	Dir(string) string

	// Join joins any number of path elements into a single path.
	// Different filepath.Dir, it won't clean "..".
	Join(...string) string

	// Rel returns a relative path that is lexically equivalent
	// to targpath when joined to basepath with an intervening separator.
	Rel(basepath, targpath string) (string, error)

	// Clean returns the shortest path name equivalent to path by purely lexical processing.
	Clean(path string) string

	// SplitElem splits path by path separator.
	SplitElem(path string) []string

	// PathSep returns the path separator.
	PathSep() string
}

// FilePathOf returns FilePath of path type.
func FilePathOf(pt pb.CmdDescriptor_PathType) (FilePath, error) {
	switch pt {
	case pb.CmdDescriptor_POSIX:
		return posixpath.FilePath{}, nil

	case pb.CmdDescriptor_WINDOWS:
		return winpath.FilePath{}, nil
	}
	return nil, fmt.Errorf("bad path type: %s", pt)
}

// IsRelocatable returns true if this setup can be relocated to where client
// put a compiler or a subprogram.
// setup must have valid path type.
func IsRelocatable(setup *pb.CmdDescriptor_Setup) (bool, error) {
	filepath, err := FilePathOf(setup.GetPathType())
	if err != nil {
		return false, fmt.Errorf("setup %s: %v", setup, err)
	}
	if filepath.IsAbs(setup.CmdFile.Path) {
		return false, nil
	}
	return true, nil
}

// AbsCmdPath returns absolute command path of the setup.
func AbsCmdPath(setup *pb.CmdDescriptor_Setup) (string, error) {
	filepath, err := FilePathOf(setup.GetPathType())
	if err != nil {
		return "", err
	}
	cmdpath := setup.CmdFile.Path
	if !filepath.IsAbs(setup.CmdFile.Path) {
		cmdpath = filepath.Join(setup.CmdDir, setup.CmdFile.Path)
	}
	return cmdpath, nil
}

// Relocate relocates cmdpath and setup files.
func Relocate(setup *pb.CmdDescriptor_Setup, cmdpath string) (*pb.CmdDescriptor_Setup, error) {
	filepath, err := FilePathOf(setup.GetPathType())
	if err != nil {
		return nil, err
	}
	s := proto.Clone(setup).(*pb.CmdDescriptor_Setup)
	origin := filepath.Dir(cmdpath)
	s.CmdFile.Path = cmdpath
	for _, f := range s.Files {
		if filepath.IsAbs(f.Path) {
			continue
		}
		f.Path = filepath.Join(origin, f.Path)
	}
	return s, nil
}

// relocateCommands returns relocated command files.
// cmdPath is a command path (relative client path).
// cs is (main) command setup.
//
// cmdseen is used to filter out the existing files from the result.
func relocateCommands(cmdseen map[string]bool, cmdPath string, cs *pb.CmdDescriptor_Setup) ([]*pb.FileSpec, error) {
	// Note: paths in cmdseen is a client form, and can be a relative path.
	// So, this code cannot dedup the subprograms exhaustively.

	var result []*pb.FileSpec

	cs, err := Relocate(cs, cmdPath)
	if err != nil {
		return nil, err
	}
	if !cmdseen[cs.CmdFile.Path] {
		result = append(result, cs.CmdFile)
		cmdseen[cs.CmdFile.Path] = true
	}

	for _, f := range cs.Files {
		if cmdseen[f.Path] {
			continue
		}
		result = append(result, f)
		cmdseen[f.Path] = true
	}

	return result, nil
}

// RelocateCmd relocates main program and subprograms if necessary.
// subprogSetups is a map from subprogram client path to subprogram setup.
// It returns commands' FileSpec to be used.
// Actual command path will be first FileSpec's Path.
func RelocateCmd(cmdPath string, setup *pb.CmdDescriptor_Setup, subprogSetups map[string]*pb.CmdDescriptor_Setup) ([]*pb.FileSpec, error) {
	// If relocatable, set up filespecs so that the command is installed in the command path.
	// If not relocatable, rewrite command itself.

	cmdseen := make(map[string]bool)
	var allRelocatedFileSpecs []*pb.FileSpec

	// Relocate the main binary
	relocatable, err := IsRelocatable(setup)
	if err != nil {
		return nil, err
	}
	if relocatable {
		relocatedFileSpecs, err := relocateCommands(cmdseen, cmdPath, setup)
		if err != nil {
			return nil, err
		}
		allRelocatedFileSpecs = append(allRelocatedFileSpecs, relocatedFileSpecs...)
	} else {
		cmdfile := proto.Clone(setup.CmdFile).(*pb.FileSpec)
		cmdfile.Path = cmdPath
		allRelocatedFileSpecs = append(allRelocatedFileSpecs,
			cmdfile)
		allRelocatedFileSpecs = append(allRelocatedFileSpecs,
			setup.Files...)
	}

	// Relocate subprograms.
	for sPath, subprogSetup := range subprogSetups {
		relocatable, err := IsRelocatable(subprogSetup)
		if err != nil {
			return nil, err
		}
		if relocatable {
			relocatedFileSpecs, err := relocateCommands(cmdseen, sPath, subprogSetup)
			if err != nil {
				return nil, err
			}
			allRelocatedFileSpecs = append(allRelocatedFileSpecs, relocatedFileSpecs...)
		} else {
			// Since the subprogram is invoked from the main program, it's hard to support it.
			// We cannot change the command line in the main program.
			//
			// Not sure there is a case that the main program calls subprogram with the fixed path.
			// If not, we can return an error here.
			//
			// However, usually the subprogram is called with relative path, or called based on
			// -B (e.g. objcopy from clang).
			cmdfile := proto.Clone(subprogSetup.CmdFile).(*pb.FileSpec)
			cmdfile.Path = sPath
			allRelocatedFileSpecs = append(allRelocatedFileSpecs,
				cmdfile)
			allRelocatedFileSpecs = append(allRelocatedFileSpecs,
				subprogSetup.Files...)
		}
	}
	return allRelocatedFileSpecs, nil
}
