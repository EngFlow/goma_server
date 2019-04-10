// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"errors"
	"fmt"
	"strings"
)

// TODO: share exec/gcc.go ?

// gccCwdAgnostic checks if args will generate cwd-agnostic outputs
// (files/stdout/stderr).
//
// args will be cwd-agnostic if path used in arg is cwd relative.
//
// args will NOT generate cwd-agnostic output (.o), that is, generate
// outputs that would contain absolute path names (DW_AT_comp_dir etc),
// if
//  debug build (-g* except -g0) -> DW_AT_comp_dir or other filepaths.
//  --pnacl-allow-translate  crbug.com/685461
//
// these flags would emit non cwd-agnostic output.
//  absolute input filename (debug build)
//      *.d file output will not be cwd-agnostic.
//      DW_AT_name (debug build)
//  -I<path>
//      *.d file output will not be cwd-agnostic.
//      directory table in debug info (debug build)
//  -B<path>
//  -isystem<path> --sysroot=<path>
//  ...
//  TODO: these could be normalized to cwd relative?
//
// ref:
// https://docs.google.com/spreadsheets/d/1_-ZJhqy7WhSFYuZU2QkmQ4Ed9182bWfKg09EfBAkVf8/edit#gid=759603323
func gccCwdAgnostic(filepath clientFilePath, args, envs []string) error {
	var debugFlags []string
	subArgs := map[string][]string{}
	pathFlag := false
	var subCmd string
	for _, arg := range args {
		switch {
		case pathFlag:
			if filepath.IsAbs(arg) {
				return fmt.Errorf("abs path: %s", arg)
			}
			pathFlag = false

		case subCmd != "":
			subArgs[subCmd] = append(subArgs[subCmd], arg)
			subCmd = ""

		case strings.HasPrefix(arg, "-g"):
			if arg == "-g0" {
				debugFlags = nil
				continue
			}
			debugFlags = append(debugFlags, arg)
		case arg == "--pnacl-allow-translate": // crbug.com/685461
			return errors.New("pnacl-allow-translate")

		case strings.HasPrefix(arg, "-Wa,"): // assembler arg
			subArgs["as"] = append(subArgs["as"], strings.Split(arg[4:], ",")...)
		case strings.HasPrefix(arg, "-Wl,"): // linker arg
			subArgs["ld"] = append(subArgs["ld"], strings.Split(arg[4:], ",")...)
		case strings.HasPrefix(arg, "-Wp,"): // preproc arg
			subArgs["cpp"] = append(subArgs["cpp"], strings.Split(arg[4:], ",")...)
		case arg == "-Xclang":
			subCmd = "clang"

		case strings.HasPrefix(arg, "-w"): // inhibit all warnings
		case strings.HasPrefix(arg, "-W"): // warning
		case strings.HasPrefix(arg, "-D"): // define
		case strings.HasPrefix(arg, "-U"): // undefine
		case strings.HasPrefix(arg, "-O"): // optimize
		case strings.HasPrefix(arg, "-f"): // feature
		case strings.HasPrefix(arg, "-m"):
			// -m64, -march=x86-64
		case arg == "-arch":
		case strings.HasPrefix(arg, "--target="):

		case strings.HasPrefix(arg, "-no"):
			// -no-canonical-prefixes, -nostdinc++
		case arg == "-integrated-as":
		case arg == "-pedantic":
		case arg == "-pipe":
		case arg == "-pthread":
		case arg == "-c":
		case strings.HasPrefix(arg, "-std"):
		case strings.HasPrefix(arg, "--param="):
		case arg == "-MMD" || arg == "-MD" || arg == "-M":
			continue

		case arg == "-o":
			pathFlag = true
		case arg == "-I" || arg == "-B" || arg == "-isystem":
			pathFlag = true
		case arg == "-MF":
			pathFlag = true
		case strings.HasPrefix(arg, "-o"):
			if filepath.IsAbs(arg[2:]) {
				return fmt.Errorf("abs path: %s", arg)
			}
		case strings.HasPrefix(arg, "-I") || strings.HasPrefix(arg, "-B"):
			if filepath.IsAbs(arg[2:]) {
				return fmt.Errorf("abs path: %s", arg)
			}
		case strings.HasPrefix(arg, "-isystem"):
			if filepath.IsAbs(arg[8:]) {
				return fmt.Errorf("abs path: %s", arg)
			}
		case strings.HasPrefix(arg, "--sysroot="):
			if filepath.IsAbs(arg[11:]) {
				return fmt.Errorf("abs path: %s", arg)
			}
		case arg == "-isysroot":
			pathFlag = true

		case strings.HasPrefix(arg, "-"): // unknown flag?
			return fmt.Errorf("unknown flag: %s", arg)

		default: // input file?
			if filepath.IsAbs(arg) {
				return fmt.Errorf("abs path: %s", arg)
			}
		}
	}

	if len(debugFlags) > 0 {
		return fmt.Errorf("debug build: %q", debugFlags)
	}
	if len(subArgs) > 0 {
		for cmd, args := range subArgs {
			switch cmd {
			case "clang":
				err := clangArgCwdAgnostic(filepath, args)
				if err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported subcommand args %s: %s", cmd, args)
			}
		}
	}

	for _, env := range envs {
		e := strings.SplitN(env, "=", 2)
		if len(e) != 2 {
			return fmt.Errorf("bad environment variable: %s", env)
		}
		if e[0] == "PWD" {
			continue
		}
		if filepath.IsAbs(e[1]) {
			return fmt.Errorf("abs path in env %s=%s", e[0], e[1])
		}
	}
	return nil
}

func clangArgCwdAgnostic(filepath clientFilePath, args []string) error {
	pathFlag := false
	skipFlag := false
	for _, arg := range args {
		switch {
		case pathFlag:
			if filepath.IsAbs(arg) {
				return fmt.Errorf("clang abs path: %s", arg)
			}
			pathFlag = false
		case skipFlag:
			skipFlag = false

		case arg == "-mllvm" || arg == "-add-plugin":
			skipFlag = true
		case strings.HasPrefix(arg, "-plugin-arg-"):
			skipFlag = true
		case arg == "-load":
			pathFlag = true
		default:
			return fmt.Errorf("clang unknown arg: %s", arg)
		}
	}
	return nil
}

// gccOutputs returns output files from gcc command line.
// TODO: implicit obj output (without -o, but -c).
// TODO: -MD / -MMD without -MF case.
func gccOutputs(args []string) []string {
	var outputs []string
	var objout string
	outputArg := false
	splitDwarf := false
	mfArg := false

	for _, arg := range args {
		switch {
		case arg == "-o":
			outputArg = true
		case outputArg:
			objout = arg
			outputArg = false
		case strings.HasPrefix(arg, "-o"):
			objout = arg[2:]

		case arg == "-gsplit-dwarf":
			splitDwarf = true

		case arg == "-MF":
			mfArg = true
		case mfArg:
			outputs = append(outputs, arg)
			mfArg = false
		case strings.HasPrefix(arg, "-MF"):
			outputs = append(outputs, arg[3:])

		}
	}
	if objout != "" {
		outputs = append(outputs, objout)
		if splitDwarf {
			outputs = append(outputs, strings.TrimSuffix(objout, ".o")+".dwo")
		}
	}
	return outputs
}
