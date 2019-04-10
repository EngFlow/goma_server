// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package descriptor

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.chromium.org/goma/server/hash"
	pb "go.chromium.org/goma/server/proto/command"
)

var (
	// subprogs is subprograms for compiler.
	// CompilerInfoBuilder::LookupAndUpdate.
	subprogs = []string{
		"cpp", "cc1", "cc1plus", "as", "objcopy",
		"ld", "nm", "strip", "cc1obj", "collect2",
		"lto1", "lto-wrapper",
	}

	clexeVersionRegexp = regexp.MustCompile(`Compiler Version ([0-9\.]+) for`)
	clexeTargetRegexp  = regexp.MustCompile(`Compiler Version [0-9\.]+ for (x\d+)`)

	clangclExpectedVersionRegexp = regexp.MustCompile(`clang\s+version\s+\d+\.\d+\.\d+\s+\(trunk\s+\d+\)`)
	clangclExpectedTargetRegexp  = regexp.MustCompile(`Target:\s+\.*`)
)

// Descriptor represents a command descriptor.
type Descriptor struct {
	// fname is command filename in server path. relative to cwd (prefix).
	fname string
	*pb.CmdDescriptor

	Runner       Runner
	ToClientPath func(string) (string, error)
	PathType     pb.CmdDescriptor_PathType

	cwd    string // current working directory in server path.
	cmddir string // abs command directory in server path.
	seen   map[string]bool
}

// Config used for setting up cmd descriptor.
type Config struct {
	Key                    string // Key used for selection.
	Filename               string // relative to pwd (prefix) in server path
	AbsoluteBinaryHashFrom string // use Filename for binary hash if emtpy
	Target                 string // cross target if specified

	Runner       Runner
	ToClientPath func(string) (string, error)
	PathType     pb.CmdDescriptor_PathType

	// compiler specific
	// https://clang.llvm.org/docs/CrossCompilation.html#target-triple
	ClangNeedTarget bool // true if -target is needed.
}

// Runner runs command and get combined output.
type Runner func(cmds ...string) ([]byte, error)

func (c Config) binaryHashFrom() string {
	if c.AbsoluteBinaryHashFrom != "" {
		return c.AbsoluteBinaryHashFrom
	}
	return c.Filename
}

// canGetTargetFromFilename returns true if target can be get from c.Filename.
// if c.AbsoluteBinaryHashFrom is given, we suppose that target cannot be get
// from either of Filename or AbsoluteBinaryHashFrom, the function returns
// false.
func (c Config) canGetTargetFromFilename() bool {
	return c.AbsoluteBinaryHashFrom == ""
}

func (c Config) isValid() bool {
	if c.Filename == "" {
		return false
	}
	// if Target is set, we expect the config is for cross compiling.
	// then, we require the config have AbsoluteBinaryHashFrom.
	// subprogram/plugin may have AbsoluteBinaryHashFrom but they do not
	// have Target.
	return c.Target == "" || c.AbsoluteBinaryHashFrom != ""
}

func cmdDescriptor(c Config) (*pb.CmdDescriptor, error) {
	sel, err := selector(c)
	if err != nil {
		return nil, err
	}
	cd := &pb.CmdDescriptor{
		Selector:      sel,
		Cross:         &pb.CmdDescriptor_Cross{},
		EmulationOpts: &pb.CmdDescriptor_EmulationOpts{},
	}

	if c.ClangNeedTarget {
		switch sel.Name {
		case "clang", "clang++", "clang-cl":
			cd.Cross.ClangNeedTarget = true
		default:
			return nil, fmt.Errorf("need_target=true for non clang: %s", sel.Name)
		}
	}

	// When filename starts with "/", it means the binary is specified with absolute path.
	// It implies the binary is not relocatable.
	// In this case, we need to respect include paths sent from the goma client,
	// since the system include paths can be different (it's often based on compiler path).
	if strings.HasPrefix(c.Filename, "/") {
		cd.EmulationOpts.RespectClientIncludePaths = true
	}

	return cd, nil
}

// New creates cmd descriptor for c.
func New(c Config) (*Descriptor, error) {
	if !c.isValid() {
		return nil, fmt.Errorf("invalid config %v", c)
	}
	cd, err := cmdDescriptor(c)
	if err != nil {
		return nil, err
	}

	return &Descriptor{
		fname:         c.Filename,
		CmdDescriptor: cd,
		Runner:        c.Runner,
		ToClientPath:  c.ToClientPath,
		PathType:      c.PathType,
		seen:          make(map[string]bool),
	}, nil
}

// filespec is file spec represetned in server path.
type filespec struct {
	Path         string // in server path.
	Symlink      string
	Hash         string
	Size         int64
	IsExecutable bool
}

// newFilespec creates filespec from fname in server path.
func newFilespec(fname string) (filespec, error) {
	fi, err := os.Lstat(fname)
	if err != nil {
		return filespec{}, err
	}
	if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
		linkname, err := os.Readlink(fname)
		if err != nil {
			return filespec{}, err
		}
		return filespec{
			Path:    fname,
			Symlink: linkname,
		}, nil
	}
	sha256, err := hash.SHA256File(fname)
	if err != nil {
		return filespec{}, err
	}
	return filespec{
		Path:         fname,
		Hash:         sha256,
		Size:         fi.Size(),
		IsExecutable: fi.Mode()&0111 != 0,
	}, nil
}

func (d *Descriptor) filespecProto(fs filespec) (*pb.FileSpec, error) {
	cpath, err := d.ToClientPath(fs.Path)
	if err != nil {
		return nil, err
	}
	return &pb.FileSpec{
		Path:         cpath,
		Symlink:      fs.Symlink,
		Hash:         fs.Hash,
		Size:         fs.Size,
		IsExecutable: fs.IsExecutable,
	}, nil
}

// appendFileSpec appends filespec to Setup.Files.
func (d *Descriptor) appendFileSpec(fs filespec) error {
	p := fs.Path
	if !filepath.IsAbs(p) {
		p = filepath.Join(d.cmddir, p)
	}
	if d.seen[p] {
		return nil
	}
	cfs, err := d.filespecProto(fs)
	if err != nil {
		return err
	}
	d.Setup.Files = append(d.Setup.Files, cfs)
	d.seen[p] = true
	return nil
}

// CmdSetup sets up command descriptor.
func (d *Descriptor) CmdSetup() error {
	if !filepath.IsAbs(d.fname) {
		return d.relocCmdSetup()
	}
	fs, err := newFilespec(d.fname)
	if err != nil {
		return err
	}
	if fs.Symlink == "" && !fs.IsExecutable {
		return fmt.Errorf("not executable: %s", d.fname)
	}
	cfs, err := d.filespecProto(fs)
	if err != nil {
		return err
	}
	d.Setup = &pb.CmdDescriptor_Setup{
		CmdFile:  cfs,
		PathType: d.PathType,
	}
	d.seen[fs.Path] = true
	if fs.Symlink != "" {
		err = d.resolveSymlinks("", fs)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *Descriptor) relocCmdSetup() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	d.cwd = cwd
	fs, err := newFilespec(d.fname)
	if err != nil {
		return err
	}
	if fs.Symlink == "" && !fs.IsExecutable {
		return fmt.Errorf("not executable: %s", d.fname)
	}
	cfs, err := d.filespecProto(fs)
	if err != nil {
		return err
	}
	ccwd, err := d.ToClientPath(cwd)
	if err != nil {
		return err
	}
	d.Setup = &pb.CmdDescriptor_Setup{
		CmdFile:  cfs,
		CmdDir:   ccwd,
		PathType: d.PathType,
	}
	fname := filepath.Join(cwd, d.fname)
	d.seen[fname] = true
	d.cmddir = filepath.Dir(fname)

	fs.Path = fname
	err = d.resolveSymlinks(d.cmddir, fs)
	if err != nil {
		return err
	}
	// ignore error, since fname may not be ELF executable.
	_ = d.relocatableLibraries(fname)

	if d.Setup.PathType == pb.CmdDescriptor_WINDOWS {
		// don't detect subprograms for windows clang
		// (e.g. pnacl-clang), because compilePath doesn't work.
		// set them in files explicitly.
		// TODO: fix?
		return nil
	}

	// Find subprograms.
	var langopt string
	switch d.CmdDescriptor.Selector.Name {
	case "gcc", "clang":
		langopt = "-xc"
	case "g++", "clang++":
		langopt = "-xc++"
	default:
		return nil
	}
	cpaths, err := compilerPath(d.fname, langopt, d.Runner)
	if err != nil {
		return err
	}
	for _, sp := range subprogs {
		spath, err := lookpath(sp, cpaths)
		if err == nil {
			// relative to cmd
			fs, err := newFilespec(spath)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(d.cmddir, fs.Path)
			if err != nil {
				return err
			}
			fs.Path = rel
			err = d.appendFileSpec(fs)
			if err != nil {
				return err
			}
			err = d.resolveSymlinks(d.cmddir, fs)
			if err != nil {
				return err
			}
			err = d.relocatableLibraries(spath)
			if err != nil {
				return err
			}
		}

		// TODO: don't put system default subprograms
		// in cmd descriptor, but put in runtime container image?
		spath, err = lookpath(sp, []string{"/usr/bin", "/bin"})
		if err == nil {
			// abspath
			fs, err := newFilespec(spath)
			if err != nil {
				return err
			}
			err = d.appendFileSpec(fs)
			if err != nil {
				return err
			}
			err = d.resolveSymlinks("", fs)
			if err != nil {
				return err
			}
			// assume libraries are in system library dir
			// so no need to setup at each compilation.
		}
	}
	return nil
}

func (d *Descriptor) resolveSymlinks(basedir string, fs filespec) error {
	if fs.Symlink == "" {
		return nil
	}
	if basedir != "" && !filepath.IsAbs(fs.Path) {
		fs.Path = filepath.Join(basedir, fs.Path)
	}
	_, err := os.Stat(fs.Path)
	if err != nil {
		return err
	}
	for fs.Symlink != "" {
		sname := fs.Symlink
		if !filepath.IsAbs(fs.Symlink) {
			sname = filepath.Join(filepath.Dir(fs.Path), fs.Symlink)
		}
		fs, err = newFilespec(sname)
		if err != nil {
			return err
		}
		if basedir != "" {
			rel, err := filepath.Rel(basedir, fs.Path)
			if err != nil {
				return err
			}
			fs.Path = rel
		}
		err = d.appendFileSpec(fs)
		if err != nil {
			return err
		}
		fs.Path = filepath.Join(basedir, fs.Path)
	}
	return nil
}

func (d *Descriptor) relocatableLibraries(cmdpath string) error {
	lpaths, err := relocatableLibraries(cmdpath)
	if err != nil {
		return err
	}
	for _, lpath := range lpaths {
		fs, err := newFilespec(lpath)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(d.cmddir, fs.Path)
		if err != nil {
			return err
		}
		fs.Path = rel
		err = d.appendFileSpec(fs)
		if err != nil {
			return err
		}
		err = d.resolveSymlinks(d.cmddir, fs)
		if err != nil {
			return err
		}
	}
	return nil
}

// Add adds fname for command.
// fname should be absolute path, or relative to cwd.
// It will emit relative path to command if cmd is relocatable.
// It just adds fname as is. i.e. not resolve symlink, resolve shared obj etc.
func (d *Descriptor) Add(fname string) error {
	if !filepath.IsAbs(fname) {
		fname = filepath.Join(d.cwd, fname)
	}
	fs, err := newFilespec(fname)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(d.fname) {
		// relocatable. fname should be relative to cmd dir.
		rel, err := filepath.Rel(d.cmddir, fs.Path)
		if err != nil {
			return err
		}
		fs.Path = rel
	}
	return d.appendFileSpec(fs)
}

func lookpath(fname string, paths []string) (string, error) {
	for _, path := range paths {
		name := filepath.Join(path, fname)
		_, err := os.Stat(name)
		if err != nil {
			continue
		}
		return name, nil
	}
	return "", fmt.Errorf("not found %s in %v", fname, paths)
}

func compilerPath(fname, langopt string, runner Runner) ([]string, error) {
	cmds := []string{fname, "-E", "-v", langopt,
		"/dev/null", "-o", "/dev/null"}
	out, err := runner(cmds...)
	if err != nil {
		return nil, fmt.Errorf("compilerPath: %v: %v", cmds, err)
	}
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "COMPILER_PATH=") {
			line = strings.TrimPrefix(line, "COMPILER_PATH=")
			return strings.Split(line, ":"), nil
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return nil, err
}

// selector returns selector for c.
func selector(c Config) (*pb.Selector, error) {
	sha256, err := hash.SHA256File(c.binaryHashFrom())
	if err != nil {
		return nil, fmt.Errorf("binary hash for %s: %v", c.binaryHashFrom(), err)
	}
	var v, t string
	switch c.Key {
	case "gcc", "g++", "clang", "clang++":
		v, err = version(c.Filename, c.Runner)
		if err != nil {
			return nil, fmt.Errorf("version %s [%s]: %v", c.Filename, sha256, err)
		}
		if !c.canGetTargetFromFilename() {
			if c.Target == "" {
				return nil, fmt.Errorf("target not given %v", c)
			}
			t = c.Target
		} else {
			t, err = target(c.Filename, c.Runner)
			if err != nil {
				return nil, fmt.Errorf("target %s [%s]: %v", c.Filename, sha256, err)
			}
		}
	case "javac": // TODO: also java?
		v, err = javaVersion(c.Filename, c.Runner)
		if err != nil {
			return nil, fmt.Errorf("version %s [%s]: %v", c.Filename, sha256, err)
		}
		// javac's target is set to java in goma client.
		t = "java"
	case "cl.exe":
		v, err = clexeVersion(c.Filename, c.Runner)
		if err != nil {
			return nil, fmt.Errorf("version %s [%s]: %v", c.Filename, sha256, err)
		}
		t, err = clexeTarget(c.Filename, c.Runner)
		if err != nil {
			return nil, fmt.Errorf("target %s [%s]: %v", c.Filename, sha256, err)
		}
	case "clang-cl":
		v, err = clangclVersion(c.Filename, c.Runner)
		if err != nil {
			return nil, fmt.Errorf("version %s [%s]: %v", c.Filename, sha256, err)
		}
		if !c.canGetTargetFromFilename() {
			if c.Target == "" {
				return nil, fmt.Errorf("target not given %v", c)
			}
			t = c.Target
		} else {
			t, err = clangclTarget(c.Filename, c.Runner)
			if err != nil {
				return nil, fmt.Errorf("target %s [%s]: %v", c.Filename, sha256, err)
			}
		}
	default:
		// subprogram would not have version,target (?)
	}
	return &pb.Selector{
		Name:       c.Key,
		Version:    v,
		Target:     t,
		BinaryHash: sha256,
	}, nil
}

func version(cmd string, runner Runner) (string, error) {
	dout, err := runner(cmd, "-dumpversion")
	if err != nil {
		return "", fmt.Errorf("failed to take %s dump version: %v", cmd, err)
	}
	vout, err := runner(cmd, "--version")
	if err != nil {
		return "", fmt.Errorf("failed to take %s version: %v", cmd, err)
	}
	return Version(dout, vout), nil
}

// Version returns version field for command spec/selector
// from -dumpversion output and --version output.
func Version(dout, vout []byte) string {
	dout = firstLine(dout)
	vout = normalize(firstLine(vout))
	return fmt.Sprintf("%s[%s]", dout, vout)
}

func normalize(v []byte) []byte {
	i := bytes.IndexByte(v, '(')
	if i < 0 {
		return v
	}
	progname := v[:i]
	if bytes.Contains(progname, []byte("clang")) {
		return v
	}
	if !bytes.Contains(progname, []byte("g++")) && !bytes.Contains(progname, []byte("gcc")) {
		return v
	}
	return v[i:]
}

func target(cmd string, runner Runner) (string, error) {
	out, err := runner(cmd, "-dumpmachine")
	if err != nil {
		return "", fmt.Errorf("failed to take %s dump machine: %v", cmd, err)
	}
	return Target(out), nil
}

// Target returns target field of command spec/selector
// from -dumpmachine output.
func Target(out []byte) string {
	return string(firstLine(out))
}

func firstLine(b []byte) []byte {
	i := bytes.IndexAny(b, "\r\n")
	if i >= 0 {
		b = b[:i]
	}
	return b
}

func javaVersion(cmd string, runner Runner) (string, error) {
	out, err := runner(cmd, "-version")
	if err != nil {
		return "", fmt.Errorf("failed to get version: %s: %s %v", cmd, out, err)
	}
	return JavaVersion(out)
}

// JavaVersion returns version string of javac.
func JavaVersion(out []byte) (string, error) {
	javac := []byte("javac ")
	if !bytes.HasPrefix(out, javac) {
		return "", fmt.Errorf("not starts with javac: %s", out)
	}
	return string(bytes.TrimPrefix(firstLine(out), javac)), nil
}

func clexeVersion(cmd string, runner Runner) (string, error) {
	out, err := runner(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to take cl.exe version: %v", err)
	}
	return ClexeVersion(out)
}

// ClexeVersion returns version string of CL.exe.
func ClexeVersion(out []byte) (string, error) {
	match := clexeVersionRegexp.FindStringSubmatch(string(out))
	if match == nil {
		return "", fmt.Errorf("could not find version string: %s", out)
	}
	if len(match) < 2 {
		return "", fmt.Errorf("unexpected version string match: %s", out)
	}
	return match[1], nil
}

func clexeTarget(cmd string, runner Runner) (string, error) {
	out, err := runner(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to take cl.exe target: %v", err)
	}
	return ClexeTarget(out)
}

// ClexeTarget returns target string of CL.exe.
func ClexeTarget(out []byte) (string, error) {
	match := clexeTargetRegexp.FindStringSubmatch(string(out))
	if match == nil {
		return "", fmt.Errorf("could not find target string: %s", out)
	}
	if len(match) < 2 {
		return "", fmt.Errorf("unexpected target string match: %s", out)
	}
	return match[1], nil
}

// ClangClVersion returns clang version from output of `clang-cl.exe -###`.
//
// `clang-cl -###` output is like the following:
//
//   clang version 3.5.0 (trunk 225621)
//   Target: i686-pc-windows-msvc
//
//   clang version 6.0.0 (trunk 308728)
//   Target: x86_64-pc-windows-msvc
//
// Note that endline might be CRLF.
func ClangClVersion(out []byte) (string, error) {
	lines := bytes.SplitN(out, []byte("\n"), 2)
	if len(lines) >= 1 && clangclExpectedVersionRegexp.Match(lines[0]) {
		return string(bytes.TrimSpace(lines[0])), nil
	}

	return "", fmt.Errorf("unexpected clang-cl output: %s", out)
}

func clangclVersion(cmd string, runner Runner) (string, error) {
	out, err := runner(cmd, "-###")
	if err != nil {
		return "", fmt.Errorf("failed to take clang-cl version: %v", err)
	}
	return ClangClVersion(out)
}

// ClangClTarget returns clang target from output of `clang-cl.exe -###`
// See ClangClVersion about the expected input.
func ClangClTarget(out []byte) (string, error) {
	lines := bytes.SplitN(out, []byte("\n"), 3)
	if len(lines) >= 2 && clangclExpectedTargetRegexp.Match(lines[1]) {
		s := lines[1][len("Target: "):]
		return string(bytes.TrimSpace(s)), nil
	}

	return "", fmt.Errorf("unexpected clang-cl output: %s", out)
}

func clangclTarget(cmd string, runner Runner) (string, error) {
	out, err := runner(cmd, "-###")
	if err != nil {
		return "", fmt.Errorf("failed to take clang-cl target: %v", err)
	}
	return ClangClTarget(out)
}
