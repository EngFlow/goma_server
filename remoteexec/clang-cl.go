// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"fmt"
	"strconv"
	"strings"
)

// TODO: share exec/clangcl.go ?

var clangClPathFlags = []string{
	"--sysroot=",
	"-B",
	"-I",
	"-fcrash-diagnostics-dir=",
	"-fprofile-instr-use=",
	"-fprofile-sample-use=",
	"-fsanitize-blacklist=",
	"-include=",
	"-isystem",
	"-o",
	"-resource-dir=",
	"-imsvc",
	"/FA",
	"/Fa",
	"/Fd",
	"/Fe",
	"/Fl",
	"/Fm",
	"/Fo",
	"/Fp",
	"/FR",
	"/Fr",
	"/FU",
	"/Fx",
}

func isClangclWarningFlag(arg string) bool {
	if len(arg) < 2 || (arg[:2] != "/W" && arg[0:2] != "/w") {
		return false
	}

	basicFlags := []string{
		"/w",                              // Suppress all warnings
		"/W0", "/W1", "/W2", "/W3", "/W4", // Warning levels 0-4
		"/Wall", // Display all warnings
		"/WX",   // Treats warnings as errors
		"/Wv",   // Display only warnings added in current compiler version
	}
	for _, flag := range basicFlags {
		if arg == flag {
			return true
		}
	}
	// Display warnings added in given compiler version
	if strings.HasPrefix(arg, "/Wv:") {
		return true
	}

	// All remaining possible warnings have the format /wXnnnn
	if len(arg) != 7 {
		return false
	}

	switch arg[2:3] {
	case "1", "2", "3", "4": // Set warning level for numbered warning
		fallthrough
	case "d": // Suppress numbered warning
		fallthrough
	case "e": // Treat numbered warning as error
		fallthrough
	case "o": // Report numbered warning only once
		// Possible warning values: 4000-5999:
		// https://docs.microsoft.com/en-us/cpp/error-messages/compiler-errors-1/c-cpp-build-errors
		warning, err := strconv.Atoi(arg[3:])
		if err != nil {
			return false
		}
		return warning >= 4000 && warning < 6000
	}

	return false
}

func isClangclOptimizationFlag(arg string) bool {
	optFlags := []string{
		"/O1",                          // Optimize for size
		"/O2",                          // Optimize for speed
		"/Ob0", "/Ob1", "/Ob2", "/Ob3", // Inline function expansino
		"/Od",         // Turn off optimization
		"/Og",         // Global optimization
		"/Oi", "/Oi-", // Intrinsic functions
		"/Os", "/Ot", // Favor small/fast code
		"/Ox",         // Enable most speed optimizations
		"/Oy", "/Oy-", // Frame pointer omission
	}
	for _, flag := range optFlags {
		if arg == flag {
			return true
		}
	}
	return false
}

// clangclRelocatableReq checks if the request (args, envs) uses relative
// paths only and doesn't use flags that generates output including cwd,
// so will generate cwd-agnostic outputs
// (files/stdout/stderr will not include cwd dependent paths).
// TODO: Combine some of this code with gccRelocatableReq.
func clangclRelocatableReq(filepath clientFilePath, args, envs []string) error {
	var debugFlags []string
	subArgs := map[string][]string{}
	var subCmd string
	pathFlag := false
Loop:
	for _, arg := range args {
		if pathFlag {
			if filepath.IsAbs(arg) {
				return fmt.Errorf("abs path: %s", arg)
			}
			pathFlag = false
			continue
		}
		for _, fp := range clangClPathFlags {
			if arg != fp && strings.HasPrefix(arg, fp) {
				if filepath.IsAbs(arg[len(fp):]) {
					return fmt.Errorf("abs path: %s", arg)
				}
				continue Loop
			}
		}
		switch {
		case subCmd != "":
			subArgs[subCmd] = append(subArgs[subCmd], arg)
			subCmd = ""

		case strings.HasPrefix(arg, "-g"):
			if arg == "-g0" {
				debugFlags = nil
				continue
			}
			debugFlags = append(debugFlags, arg)

		case strings.HasPrefix(arg, "-Wa,"): // assembler arg
			subArgs["as"] = append(subArgs["as"], strings.Split(arg[len("-Wa,"):], ",")...)
		case strings.HasPrefix(arg, "-Wl,"): // linker arg
			subArgs["ld"] = append(subArgs["ld"], strings.Split(arg[len("-Wl,"):], ",")...)
		case strings.HasPrefix(arg, "-Wp,"): // preproc arg
			subArgs["cpp"] = append(subArgs["cpp"], strings.Split(arg[len("-Wp,"):], ",")...)
		case arg == "-Xclang":
			subCmd = "clang"
		case arg == "-mllvm":
			subCmd = "llvm"

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
		case arg == "-Qunused-arguments":
			continue

		case arg == "-o":
			pathFlag = true
		case arg == "-I" || arg == "-B" || arg == "-isystem" || arg == "-include":
			pathFlag = true
		case arg == "-MF":
			pathFlag = true
		case arg == "-isysroot":
			pathFlag = true

		// MSVC/clang-cl options are based on:
		// https://docs.microsoft.com/en-us/cpp/build/reference/compiler-options-listed-alphabetically?view=vs-2019
		// https://clang.llvm.org/docs/UsersManual.html#id9
		case arg == "/nologo":
		case arg == "/Brepro", arg == "/Brepro-": // Emit an object file which can/cannot be reproduced over time
		case arg == "/FS": // Forces serialization of all writes to the program database (PDB) file through MSPDBSRV.EXE
		case arg == "/Gy", arg == "/Gy-": // Function-level linking
		case arg == "/bigobj": // Support more sections in obj file
		case arg == "/utf-8": // Set source and execution character set as UTF-8
		case arg == "/X": // Ignore standard include paths
		case arg == "/Z7", arg == "/Zi", arg == "/ZI": // Set debug format
		case arg == "/MD", arg == "/MT", arg == "/LD": // Use normal runtime library
		case arg == "/MDd", arg == "/MTd", arg == "/LDd": // Use debug runtime library
		case arg == "/Tc", arg == "/Tp", arg == "/TC", arg == "/TP": // Source file type
		case arg == "/Gd", arg == "/Gr", arg == "/Gv", arg == "/Gz": // Calling convention
		case arg == "/GR", arg == "/GR-": // Specify RTTI
		case arg == "/GS", arg == "/GS-": // Buffer security check
		case arg == "/Gr", arg == "/Gr-": // Use __fastcall calling convention
		case arg == "/Gw", arg == "/Gw-": // Optimize global data
		case arg == "/GF": // Enables string pooling
		case arg == "/c": // Compile without linking
		case arg == "/showIncludes": // List include files
		case strings.HasPrefix(arg, "/D"): // Preprocessor
		case strings.HasPrefix(arg, "/EH"): // Specify error handling
		case strings.HasPrefix(arg, "/Zc:"): // Specify compiler behavior
		case strings.HasPrefix(arg, "/arch:"): // Specify CPU architecture
		case strings.HasPrefix(arg, "/clang:"): // Clang-specific option
		case strings.HasPrefix(arg, "/guard:cf"): // Control flow guard security checks
		case strings.HasPrefix(arg, "/showIncludes:"): // List include files
		case strings.HasPrefix(arg, "/std:c++"): // Specify C++ standard
		case isClangclWarningFlag(arg): // Flags to handle warnings
		case isClangclOptimizationFlag(arg): // Flags to handle optimization
			continue

		case strings.HasPrefix(arg, "-"), strings.HasPrefix(arg, "/"): // unknown flag?
			return fmt.Errorf("unknown flag: %s", arg)

		default: // input file?
			if filepath.IsAbs(arg) {
				return fmt.Errorf("abs path: %s", arg)
			}
		}
	}

	if len(subArgs) > 0 {
		for cmd, args := range subArgs {
			switch cmd {
			case "clang":
				err := clangclArgRelocatable(filepath, args)
				if err != nil {
					return err
				}
			case "llvm":
				err := llvmArgRelocatable(filepath, args)
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

func clangclArgRelocatable(filepath clientFilePath, args []string) error {
	pathFlag := false
	skipFlag := false
	for _, arg := range args {
		switch {
		case pathFlag:
			if filepath.IsAbs(arg) {
				return fmt.Errorf("clang-cl abs path: %s", arg)
			}
			pathFlag = false
		case skipFlag:
			skipFlag = false

		case arg == "-fdebug-compilation-dir":
			pathFlag = true
		case strings.HasPrefix(arg, "-debug-info-kind"):
			continue
		case arg == "-add-plugin", arg == "-mllvm", arg == "-plugin-arg-blink-gc-plugin":
			// TODO: pass llvmArgRelocatable for -mllvm?
			skipFlag = true
			continue
		default:
			return fmt.Errorf("clang-cl unknown arg: %s", arg)
		}
	}
	return nil
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
