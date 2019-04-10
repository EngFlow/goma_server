// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	bpb "google.golang.org/genproto/googleapis/bytestream"

	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	cmdpb "go.chromium.org/goma/server/proto/command"
	fpb "go.chromium.org/goma/server/proto/file"
	rpb "go.chromium.org/goma/server/proto/remote-apis/build/bazel/remote/execution/v2"
	"go.chromium.org/goma/server/remoteexec/cas"
)

func TestAdapterHandleMissingCompiler(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	clangUnknown := newFakeClang(&cluster.cmdStorage, "1111", "x86-64-linux-gnu")
	clang := newFakeClang(&cluster.cmdStorage, "1234", "x86-64-linux-gnu")

	err = cluster.pushToolchains(ctx, clang)
	if err != nil {
		t.Fatal(err)
	}

	var localFiles fakeLocalFiles
	localFiles.Add("/b/c/w/src/hello.cc", randomSize())
	localFiles.Add("/b/c/w/include/hello.h", randomSize())

	req := &gomapb.ExecReq{
		// client requests with unknown clang for goma.
		CommandSpec: clangUnknown.CommandSpec("clang", "bin/clang"),
		Arg: []string{
			"bin/clang", "-I../../include",
			"-c", "../../src/hello.cc",
		},
		Env: []string{},
		Cwd: proto.String("/b/c/w/out/Release"),
		Input: []*gomapb.ExecReq_Input{
			localFiles.mustInput(ctx, t, nil, "/b/c/w/src/hello.cc", "../../src/hello.cc"),
			localFiles.mustInput(ctx, t, nil, "/b/c/w/include/hello.h", "../../include/hello.h"),
		},
		Subprogram:    []*gomapb.SubprogramSpec{},
		RequesterInfo: &gomapb.RequesterInfo{},
		HermeticMode:  proto.Bool(true),
	}

	resp, err := cluster.adapter.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
	}

	if resp.GetError() != gomapb.ExecResp_BAD_REQUEST {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_BAD_REQUEST)
	}
	// client CompileTask::CheckNoMatchingCommandSpec
	if resp.GetResult().CommandSpec == nil {
		t.Errorf("Exec missing command_spec")
	}
	commandSpec := resp.GetResult().GetCommandSpec()
	if commandSpec.BinaryHash != nil {
		t.Errorf("Exec command_spec.binary_hash=%q; want not set",
			string(commandSpec.BinaryHash))
	}
}

func handleMissingInputs(ctx context.Context, t *testing.T, gomaFile fpb.FileServiceClient, localFiles fakeLocalFiles, req *gomapb.ExecReq, resp *gomapb.ExecResp) {
	t.Logf("client uploads/embeds missing inputs")
	if resp.GetError() != gomapb.ExecResp_OK {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
	}
Loop:
	for i, input := range req.Input {
		for _, fname := range resp.MissingInput {
			if input.GetFilename() == fname {
				fullname := filepath.Join(req.GetCwd(), fname)
				t.Logf("upload/embed %s", fullname)
				req.Input[i] = localFiles.mustInput(ctx, t, gomaFile, fullname, fname)
				continue Loop
			}
		}
	}
}

func TestAdapterHandleMissingInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	clang := newFakeClang(&cluster.cmdStorage, "1234", "x86-64-linux-gnu")

	err = cluster.pushToolchains(ctx, clang)
	if err != nil {
		t.Fatal(err)
	}

	var localFiles fakeLocalFiles
	localFiles.Add("/b/c/w/src/hello.cc", randomSize())
	localFiles.Add("/b/c/w/include/hello.h", randomSize())

	req := &gomapb.ExecReq{
		CommandSpec: clang.CommandSpec("clang", "bin/clang"),
		Arg: []string{
			"bin/clang", "-I../../include",
			"-c", "../../src/hello.cc",
		},
		Env: []string{},
		Cwd: proto.String("/b/c/w/out/Release"),
		Input: []*gomapb.ExecReq_Input{
			// client sends hash only (fc==nil).
			localFiles.mustInput(ctx, t, nil, "/b/c/w/src/hello.cc", "../../src/hello.cc"),
			localFiles.mustInput(ctx, t, nil, "/b/c/w/include/hello.h", "../../include/hello.h"),
		},
		Subprogram:    []*gomapb.SubprogramSpec{},
		RequesterInfo: &gomapb.RequesterInfo{},
		HermeticMode:  proto.Bool(true),
	}

	t.Logf("first call")
	resp, err := cluster.adapter.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
	}
	if resp.GetError() != gomapb.ExecResp_OK {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
	}

	wantMissing := []string{"../../src/hello.cc", "../../include/hello.h"}

	if !reflect.DeepEqual(resp.MissingInput, wantMissing) {
		t.Fatalf("missing=%q; want=%q", resp.MissingInput, wantMissing)
	}
	if len(resp.MissingInput) != len(resp.MissingReason) {
		t.Fatalf("missing: len(input)=%d != len(reason)=%d", len(resp.MissingInput), len(resp.MissingReason))
	}

	handleMissingInputs(ctx, t, cluster.adapter.GomaFile, localFiles, req, resp)

	t.Logf("second call")
	resp, err = cluster.adapter.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
	}
	if resp.GetError() != gomapb.ExecResp_OK {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
	}
	if len(resp.MissingInput) > 0 {
		t.Fatalf("missing=%v; want no missing", resp.MissingInput)
	}
}

func TestAdapterHandleMissingInputContents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	clang := newFakeClang(&cluster.cmdStorage, "1234", "x86-64-linux-gnu")

	err = cluster.pushToolchains(ctx, clang)
	if err != nil {
		t.Fatal(err)
	}

	var localFiles fakeLocalFiles
	localFiles.Add("/b/c/w/src/hello.cc", randomSize())
	localFiles.Add("/b/c/w/include/hello.h", randomSize())

	// these files exists in digest cache, but not in CAS yet.
	input := localFiles.mustInput(ctx, t, nil, "/b/c/w/src/hello.cc", "../../src/hello.cc")
	cluster.redis.mustSet(ctx, t, input.GetHashKey(), localFiles.mustDigest(ctx, t, "/b/c/w/src/hello.cc"))
	input = localFiles.mustInput(ctx, t, nil, "/b/c/w/include/hello.h", "../../include/hello.h")
	cluster.redis.mustSet(ctx, t, input.GetHashKey(), localFiles.mustDigest(ctx, t, "/b/c/w/include/hello.h"))

	req := &gomapb.ExecReq{
		CommandSpec: clang.CommandSpec("clang", "bin/clang"),
		Arg: []string{
			"bin/clang", "-I../../include",
			"-c", "../../src/hello.cc",
		},
		Env: []string{},
		Cwd: proto.String("/b/c/w/out/Release"),
		Input: []*gomapb.ExecReq_Input{
			// client sends hash only (fc==nil).
			localFiles.mustInput(ctx, t, nil, "/b/c/w/src/hello.cc", "../../src/hello.cc"),
			localFiles.mustInput(ctx, t, nil, "/b/c/w/include/hello.h", "../../include/hello.h"),
		},
		Subprogram:    []*gomapb.SubprogramSpec{},
		RequesterInfo: &gomapb.RequesterInfo{},
		HermeticMode:  proto.Bool(true),
	}

	t.Logf("first call")
	// found in digest in digest cache, but no content in CAS yet.
	// return missing input instead of internal error. http://b/123546251
	resp, err := cluster.adapter.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
	}
	if resp.GetError() != gomapb.ExecResp_OK {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
	}

	wantMissing := []string{"../../src/hello.cc", "../../include/hello.h"}

	if !reflect.DeepEqual(resp.MissingInput, wantMissing) {
		t.Fatalf("missing=%q; want=%q", resp.MissingInput, wantMissing)
	}
	if len(resp.MissingInput) != len(resp.MissingReason) {
		t.Fatalf("missing: len(input)=%d != len(reason)=%d", len(resp.MissingInput), len(resp.MissingReason))
	}

	handleMissingInputs(ctx, t, cluster.adapter.GomaFile, localFiles, req, resp)

	t.Logf("second call")
	resp, err = cluster.adapter.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
	}
	if resp.GetError() != gomapb.ExecResp_OK {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
	}
	if len(resp.MissingInput) > 0 {
		t.Fatalf("missing=%v; want no missing", resp.MissingInput)
	}
}

func TestAdapterHandleSameCwdAndInputRoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	clang := newFakeClang(&cluster.cmdStorage, "1234", "x86-64-linux-gnu")

	err = cluster.pushToolchains(ctx, clang)
	if err != nil {
		t.Fatal(err)
	}

	var localFiles fakeLocalFiles
	localFiles.Add("/cwd/hello.cc", randomSize())

	cs := clang.CommandSpec("clang", "clang")

	req := &gomapb.ExecReq{
		CommandSpec: cs,
		Arg: []string{
			"./clang", "-c", "./hello.cc",
		},
		Env: []string{},
		Cwd: proto.String("/cwd"),
		Input: []*gomapb.ExecReq_Input{
			localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/cwd/hello.cc", "hello.cc"),
		},
		Subprogram:    []*gomapb.SubprogramSpec{},
		RequesterInfo: &gomapb.RequesterInfo{},
		HermeticMode:  proto.Bool(true),
	}

	resp, err := cluster.adapter.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
	}
	if resp.GetError() != gomapb.ExecResp_OK {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
	}

	command := cluster.rbe.gotCommand
	if command == nil {
		t.Fatalf("gotCommand is nil")
	}
	if len(command.Arguments) == 0 {
		t.Errorf("arguments must not be empty")
	}

	firstArg := command.Arguments[0]
	if firstArg != "./run.sh" {
		t.Errorf(`command.Arguments[0]=%q; want="./run.sh"`, firstArg)
	}

	if command.WorkingDirectory != "" {
		t.Errorf(`command.WorkingDirectory=%q; want=""`, command.WorkingDirectory)
	}
	workDirExists := false
	for _, v := range command.EnvironmentVariables {
		if v.Name == "WORK_DIR" {
			workDirExists = true
			if v.Value != "." {
				t.Errorf(`WORK_DIR=%q; want="."`, v.Value)
			}
		}
	}

	if !workDirExists {
		t.Errorf("WORK_DIR not found")
	}
	// TODO: add test case that Command.WorkingDirectory is set.
}

func TestAdapterHandleOutputs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	clang := newFakeClang(&cluster.cmdStorage, "1234", "x86-64-linux-gnu")

	err = cluster.pushToolchains(ctx, clang)
	if err != nil {
		t.Fatal(err)
	}

	var localFiles fakeLocalFiles
	localFiles.Add("/b/c/w/src/hello.cc", randomSize())
	localFiles.Add("/b/c/w/include/hello.h", randomSize())

	cs := clang.CommandSpec("clang", "bin/clang")

	req := &gomapb.ExecReq{
		CommandSpec: cs,
		Arg: []string{
			"bin/clang", "-I../../include",
			"-c", "../../src/hello.cc",
		},
		Env: []string{},
		Cwd: proto.String("/b/c/w/out/Release"),
		Input: []*gomapb.ExecReq_Input{
			localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/src/hello.cc", "../../src/hello.cc"),
			localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/include/hello.h", "../../include/hello.h"),
		},
		Subprogram:          []*gomapb.SubprogramSpec{},
		RequesterInfo:       &gomapb.RequesterInfo{},
		HermeticMode:        proto.Bool(true),
		ExpectedOutputFiles: []string{"hello.o"},
		ExpectedOutputDirs:  []string{"fake-directory"},
	}

	resp, err := cluster.adapter.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
	}
	if resp.GetError() != gomapb.ExecResp_OK {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
	}

	command := cluster.rbe.gotCommand
	if command == nil {
		t.Fatalf("gotCommand is nil")
	}

	wantOutputFiles := []string{
		"out/Release/hello.o",
	}
	wantOutputDirs := []string{
		"out/Release/fake-directory",
	}

	if !reflect.DeepEqual(command.OutputFiles, wantOutputFiles) {
		t.Errorf("output files: got=%v, want=%v", command.OutputFiles, wantOutputFiles)
	}
	if !reflect.DeepEqual(command.OutputDirectories, wantOutputDirs) {
		t.Errorf("output dirs: got=%v, want=%v", command.OutputDirectories, wantOutputDirs)
	}
}

func TestAdapterHandleOutputsWithoutExpectedOutputs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	clang := newFakeClang(&cluster.cmdStorage, "1234", "x86-64-linux-gnu")

	err = cluster.pushToolchains(ctx, clang)
	if err != nil {
		t.Fatal(err)
	}

	var localFiles fakeLocalFiles
	localFiles.Add("/b/c/w/src/hello.cc", randomSize())
	localFiles.Add("/b/c/w/include/hello.h", randomSize())

	cs := clang.CommandSpec("clang", "bin/clang")

	req := &gomapb.ExecReq{
		CommandSpec: cs,
		Arg: []string{
			"bin/clang", "-I../../include",
			"-c", "../../src/hello.cc",
			"-o", "hello.o",
		},
		Env: []string{},
		Cwd: proto.String("/b/c/w/out/Release"),
		Input: []*gomapb.ExecReq_Input{
			localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/src/hello.cc", "../../src/hello.cc"),
			localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/include/hello.h", "../../include/hello.h"),
		},
		Subprogram:    []*gomapb.SubprogramSpec{},
		RequesterInfo: &gomapb.RequesterInfo{},
		HermeticMode:  proto.Bool(true),
	}

	resp, err := cluster.adapter.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
	}
	if resp.GetError() != gomapb.ExecResp_OK {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
	}

	command := cluster.rbe.gotCommand
	if command == nil {
		t.Fatalf("gotCommand is nil")
	}

	wantOutputFiles := []string{
		"out/Release/hello.o",
	}
	var wantOutputDirs []string

	if !reflect.DeepEqual(command.OutputFiles, wantOutputFiles) {
		t.Errorf("output files: got=%v, want=%v", command.OutputFiles, wantOutputFiles)
	}
	if !reflect.DeepEqual(command.OutputDirectories, wantOutputDirs) {
		t.Errorf("output dirs: got=%v, want=%v", command.OutputDirectories, wantOutputDirs)
	}
}

func TestAdapterHandleCrossCompile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	clang := newFakeClang(&cluster.cmdStorage, "1234", "x86_64-darwin")
	for _, desc := range clang.descs {
		desc.Cross = &cmdpb.CmdDescriptor_Cross{
			ClangNeedTarget: true,
		}
	}

	err = cluster.pushToolchains(ctx, clang)
	if err != nil {
		t.Fatal(err)
	}

	var localFiles fakeLocalFiles
	localFiles.Add("/b/c/w/src/hello.cc", randomSize())
	localFiles.Add("/b/c/w/include/hello.h", randomSize())

	cs := clang.CommandSpec("clang", "bin/clang")
	cs.Target = proto.String("x86_64-apple-darwin10.6.0")

	req := &gomapb.ExecReq{
		CommandSpec: cs,
		Arg: []string{
			"bin/clang", "-I../../include",
			"-c", "../../src/hello.cc",
			"-o", "hello.o",
		},
		Env: []string{},
		Cwd: proto.String("/b/c/w/out/Release"),
		Input: []*gomapb.ExecReq_Input{
			localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/src/hello.cc", "../../src/hello.cc"),
			localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/include/hello.h", "../../include/hello.h"),
		},
		Subprogram:    []*gomapb.SubprogramSpec{},
		RequesterInfo: &gomapb.RequesterInfo{},
		HermeticMode:  proto.Bool(true),
	}

	resp, err := cluster.adapter.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
	}
	if resp.GetError() != gomapb.ExecResp_OK {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
	}

	command := cluster.rbe.gotCommand
	if command == nil {
		t.Fatalf("gotCommand is nil")
	}

	wantArguments := []string{
		"out/Release/run.sh", "bin/clang", "-I../../include",
		"-c", "../../src/hello.cc",
		"-o", "hello.o",
		"--target=x86_64-apple-darwin10.6.0",
	}
	if !reflect.DeepEqual(command.Arguments, wantArguments) {
		t.Errorf("arguments: got=%q, want=%q", command.Arguments, wantArguments)
	}
}

type fileState struct {
	isFile       bool
	isDir        bool
	isExecutable bool
}

// TODO: implement this with GetTree?
func dumpDirIter(ctx context.Context, bs bpb.ByteStreamClient, instance, dir string, d *rpb.Digest, files map[string]fileState) error {
	logger := log.FromContext(ctx)
	logger.Infof("dir:%s %s\n", dir, d)

	resname := cas.ResName(instance, d)
	var buf bytes.Buffer
	size, err := cas.Download(ctx, bs, &buf, resname)
	if err != nil {
		return fmt.Errorf("download dir:%s %s: %v", dir, d, err)
	}
	if size != d.SizeBytes {
		return fmt.Errorf("incomplete fetch %v: size=%d", d, size)
	}
	curdir := &rpb.Directory{}
	err = proto.Unmarshal(buf.Bytes(), curdir)
	if err != nil {
		return fmt.Errorf("unmarshal dir:%s %s: %v", dir, d, err)
	}
	for _, f := range curdir.Files {
		fname := filepath.Join(dir, f.Name)
		files[fname] = fileState{
			isFile:       true,
			isExecutable: f.IsExecutable,
		}
		logger.Infof("file:%s %s x:%t\n", fname, f.Digest, f.IsExecutable)
	}
	for _, subdir := range curdir.Directories {
		dname := filepath.Join(dir, subdir.Name)
		files[dname] = fileState{
			isDir: true,
		}
		err := dumpDirIter(ctx, bs, instance, dname, subdir.Digest, files)
		if err != nil {
			return err
		}
	}
	return nil
}

// dumpDirs dumps file list and directory list.
// The value of `files` means a file is executable or not.
func dumpDir(ctx context.Context, bs bpb.ByteStreamClient, instance, dir string, d *rpb.Digest) (map[string]fileState, error) {
	files := make(map[string]fileState)
	err := dumpDirIter(ctx, bs, instance, dir, d, files)
	if err != nil {
		return nil, err
	}
	return files, nil
}

func TestAdapterHandleOutputsWithSystemIncludePaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	clang := newFakeClang(&cluster.cmdStorage, "1234", "x86-64-linux-gnu")

	err = cluster.pushToolchains(ctx, clang)
	if err != nil {
		t.Fatal(err)
	}

	var localFiles fakeLocalFiles
	localFiles.Add("/b/c/w/src/hello.cc", randomSize())
	localFiles.Add("/b/c/w/include/hello.h", randomSize())

	cs := clang.CommandSpec("clang", "bin/clang")

	req := &gomapb.ExecReq{
		CommandSpec: cs,
		Arg: []string{
			"bin/clang", "-I../../include",
			"--sysroot=../../build/linux/debian_sid_amd64-sysroot",
			"-c", "../../src/hello.cc",
			"-o", "hello.o",
		},
		Env: []string{},
		Cwd: proto.String("/b/c/w/out/Release"),
		Input: []*gomapb.ExecReq_Input{
			localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/src/hello.cc", "../../src/hello.cc"),
			localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/include/hello.h", "../../include/hello.h"),
		},
		Subprogram:    []*gomapb.SubprogramSpec{},
		RequesterInfo: &gomapb.RequesterInfo{},
		HermeticMode:  proto.Bool(true),
	}
	req.CommandSpec.SystemIncludePath = []string{
		"../../build/linux/debian_sid_amd64-sysroot/usr/include/x86_64-linux-gnu",
	}

	resp, err := cluster.adapter.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
	}
	if resp.GetError() != gomapb.ExecResp_OK {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
	}

	action := cluster.rbe.gotAction
	if action == nil {
		t.Fatalf("gotAction is nil")
	}
	files, err := dumpDir(ctx, cluster.adapter.Client, cluster.adapter.DefaultInstance(), ".", action.InputRootDigest)
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if !files["build/linux/debian_sid_amd64-sysroot/usr/include/x86_64-linux-gnu"].isDir {
		t.Errorf("want CAS has build/linux/debian_sid_amd64-sysroot/usr/include/x86_64-linux-gnu; files=%v", files)
	}
}

func TestAdaptorHandleArbitraryToolchainSupport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cluster := &fakeCluster{
		rbe: newFakeRBE(),
	}
	err := cluster.setup(ctx, cluster.rbe.instancePrefix)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.teardown()

	// Instead of adding a new compiler, register toolchain platform.
	err = cluster.pushPlatform(ctx, "docker://grpc.io/goma-dev/container-image@sha256:yyyy", []string{"os:linux"})
	if err != nil {
		t.Fatal(err)
	}

	var localFiles fakeLocalFiles
	localFiles.Add("/b/c/w/bin/clang", randomBigSize())
	localFiles.Add("/b/c/w/include/hello.h", randomSize())
	localFiles.Add("/b/c/w/src/hello.c", randomSize())

	clangToolchainInput := localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/bin/clang", "../../bin/clang")
	clangHashKey := localFiles.mustFileHash(ctx, t, "/b/c/w/bin/clang")

	req := &gomapb.ExecReq{
		CommandSpec: &gomapb.CommandSpec{
			Name:              proto.String("clang"),
			Version:           proto.String("1234"),
			Target:            proto.String("x86-64-linux-gnu"),
			BinaryHash:        []byte(clangHashKey),
			LocalCompilerPath: proto.String("../../bin/clang"),
		},
		Arg: []string{
			"../../bin/clang", "-Iinclude",
			"-c", "../../src/hello.c",
			"-o", "hello.o",
		},
		Env: []string{},
		Cwd: proto.String("/b/c/w/out/Release"),
		Input: []*gomapb.ExecReq_Input{
			clangToolchainInput,
			localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/include/hello.h", "../../include/hello.h"),
			localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/src/hello.c", "../../src/hello.c"),
		},
		Subprogram:        []*gomapb.SubprogramSpec{},
		ToolchainIncluded: proto.Bool(true),
		ToolchainSpecs: []*gomapb.ToolchainSpec{
			&gomapb.ToolchainSpec{
				Path:         proto.String("../../bin/clang"),
				Hash:         proto.String(clangHashKey),
				Size:         clangToolchainInput.Content.FileSize,
				IsExecutable: proto.Bool(true),
			},
		},
		RequesterInfo: &gomapb.RequesterInfo{
			Dimensions: []string{
				"os:linux",
			},
			PathStyle: gomapb.RequesterInfo_POSIX_STYLE.Enum(),
		},
		ExpectedOutputFiles: []string{
			"hello.o",
		},
	}

	resp, err := cluster.adapter.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
	}
	if resp.GetError() != gomapb.ExecResp_OK {
		t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
	}

	action := cluster.rbe.gotAction
	if action == nil {
		t.Fatalf("gotAction is nil")
	}
	files, err := dumpDir(ctx, cluster.adapter.Client, cluster.adapter.DefaultInstance(), ".", action.InputRootDigest)
	if err != nil {
		t.Fatalf("err %v", err)
	}

	// files and executables might contain extra "out/Release/run.sh".
	wantFiles := []string{"bin/clang", "include/hello.h", "src/hello.c"}
	wantExecutables := []string{"bin/clang"}

	for _, f := range wantFiles {
		if !files[f].isFile {
			t.Errorf("%q was not found in files, but should: files=%v", f, files)
		}
	}
	for _, e := range wantExecutables {
		if !files[e].isExecutable {
			t.Errorf("%q was not an executable file, but should: files=%v", e, files)
		}
	}
}

func TestAdapterDockerProperties(t *testing.T) {
	for _, tc := range []struct {
		desc string
		args []string
		want []*rpb.Platform_Property
	}{
		{
			desc: "cwd agnostic",
			args: nil,
			want: nil,
		},
		{
			desc: "non cwd agnostic",
			args: []string{"-g"},
			want: []*rpb.Platform_Property{
				{
					Name:  "dockerSiblingContainers",
					Value: "true",
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			cluster := &fakeCluster{
				rbe: newFakeRBE(),
			}
			err := cluster.setup(ctx, cluster.rbe.instancePrefix)
			if err != nil {
				t.Fatal(err)
			}
			defer cluster.teardown()
			clang := newFakeClang(&cluster.cmdStorage, "1234", "x86-64-linux-gnu")
			err = cluster.pushToolchains(ctx, clang)
			if err != nil {
				t.Fatal(err)
			}
			var localFiles fakeLocalFiles
			localFiles.Add("/b/c/w/src/hello.cc", randomSize())

			req := &gomapb.ExecReq{
				CommandSpec: clang.CommandSpec("clang", "bin/clang"),
				Arg:         append([]string{"bin/clang", "-c", "../../src/hello.cc"}, tc.args...),
				Env:         []string{},
				Cwd:         proto.String("/b/c/w/out/Release"),
				Input: []*gomapb.ExecReq_Input{
					localFiles.mustInput(ctx, t, cluster.adapter.GomaFile, "/b/c/w/src/hello.cc", "../../src/hello.cc"),
				},
				Subprogram:    []*gomapb.SubprogramSpec{},
				RequesterInfo: &gomapb.RequesterInfo{},
				HermeticMode:  proto.Bool(true),
			}
			resp, err := cluster.adapter.Exec(ctx, req)
			if err != nil {
				t.Fatalf("Exec(ctx, req)=%v; %v; want nil error", resp, err)
			}
			if resp.GetError() != gomapb.ExecResp_OK {
				t.Errorf("Exec error=%v; want=%v", resp.GetError(), gomapb.ExecResp_OK)
			}

			command := cluster.rbe.gotCommand
			if command == nil {
				t.Fatalf("gotCommand is nil")
			}
			want := []*rpb.Platform_Property{}
			for _, p := range clang.RemoteexecPlatform.Properties {
				want = append(want, &rpb.Platform_Property{
					Name:  p.Name,
					Value: p.Value,
				})
			}
			want = append(want, tc.want...)
			sort.Slice(want, func(i, j int) bool {
				return want[i].Name < want[j].Name
			})
			if diff := cmp.Diff(want, command.Platform.GetProperties(), cmp.Comparer(proto.Equal)); diff != "" {
				t.Errorf("platform.Properties diff want->got\n%s", diff)
			}
		})
	}
}
