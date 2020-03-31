// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package remoteexec

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	rpb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	tspb "github.com/golang/protobuf/ptypes/timestamp"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/command/descriptor"
	"go.chromium.org/goma/server/exec"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	cmdpb "go.chromium.org/goma/server/proto/command"
	"go.chromium.org/goma/server/remoteexec/cas"
	"go.chromium.org/goma/server/remoteexec/digest"
	"go.chromium.org/goma/server/remoteexec/merkletree"
	"go.chromium.org/goma/server/rpc"
)

type request struct {
	f        *Adapter
	gomaReq  *gomapb.ExecReq
	gomaResp *gomapb.ExecResp

	client Client
	cas    *cas.CAS

	cmdConfig *cmdpb.Config
	cmdFiles  []*cmdpb.FileSpec

	digestStore *digest.Store
	tree        *merkletree.MerkleTree

	filepath clientFilePath

	args         []string
	envs         []string
	outputs      []string
	outputDirs   []string
	platform     *rpb.Platform
	action       *rpb.Action
	actionDigest *rpb.Digest

	allowChroot bool
	needChroot  bool

	err error
}

type clientFilePath interface {
	IsAbs(path string) bool
	Base(path string) string
	Dir(path string) string
	Join(elem ...string) string
	Rel(basepath, targpath string) (string, error)
	Clean(path string) string
	SplitElem(path string) []string
	PathSep() string
}

func doNotCache(req *gomapb.ExecReq) bool {
	switch req.GetCachePolicy() {
	case gomapb.ExecReq_LOOKUP_AND_STORE, gomapb.ExecReq_STORE_ONLY, gomapb.ExecReq_LOOKUP_AND_STORE_SUCCESS:
		return false
	default:
		return true
	}
}

func skipCacheLookup(req *gomapb.ExecReq) bool {
	switch req.GetCachePolicy() {
	case gomapb.ExecReq_STORE_ONLY:
		return true
	default:
		return false
	}
}

// Takes an input of environment flag defs, e.g. FLAG_NAME=value, and returns an array of
// rpb.Command_EnvironmentVariable with these flag names and values.
func createEnvVars(ctx context.Context, envs []string) []*rpb.Command_EnvironmentVariable {
	envMap := make(map[string]string)
	logger := log.FromContext(ctx)
	for _, env := range envs {
		e := strings.SplitN(env, "=", 2)
		key, value := e[0], e[1]
		storedValue, ok := envMap[key]
		if ok {
			logger.Infof("Duplicate env var: %s=%s => %s", key, storedValue, value)
		}
		envMap[key] = value
	}

	// EnvironmentVariables must be lexicographically sorted by name.
	var envKeys []string
	for k := range envMap {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)

	var envVars []*rpb.Command_EnvironmentVariable
	for _, k := range envKeys {
		envVars = append(envVars, &rpb.Command_EnvironmentVariable{
			Name:  k,
			Value: envMap[k],
		})
	}
	return envVars
}

// ID returns compiler proxy id of the request.
func (r *request) ID() string {
	return r.gomaReq.GetRequesterInfo().GetCompilerProxyId()
}

// Err returns error of the request.
func (r *request) Err() error {
	switch status.Code(r.err) {
	case codes.OK:
		return nil
	case codes.Canceled, codes.DeadlineExceeded, codes.Aborted:
		// report cancel/deadline exceeded/aborted as is
		return r.err

	case codes.Unauthenticated:
		// unauthenticated happens when oauth2 access token
		// is expired during exec call.
		// e.g.
		// desc = Request had invalid authentication credentials.
		//   Expected OAuth 2 access token, login cookie or
		//   other valid authentication credential.
		//   See https://developers.google.com/identity/sign-in/web/devconsole-project.
		// report it back to caller, so caller could retry it
		// again with new refreshed oauth2 access token.
		return r.err
	default:
		return status.Errorf(codes.Internal, "exec error: %v", r.err)
	}
}

func (r *request) instanceName() string {
	basename := r.cmdConfig.GetRemoteexecPlatform().GetRbeInstanceBasename()
	if basename == "" {
		return r.f.DefaultInstance()
	}
	return path.Join(r.f.InstancePrefix, basename)
}

// getInventoryData looks up Config and FileSpec from Inventory, and creates
// execution platform properties from Config.
// It returns non-nil ExecResp for:
// - compiler/subprogram not found
// - bad path_type in command config
func (r *request) getInventoryData(ctx context.Context) *gomapb.ExecResp {
	if r.err != nil {
		return nil
	}

	logger := log.FromContext(ctx)

	cmdConfig, cmdFiles, err := r.f.Inventory.Pick(ctx, r.gomaReq, r.gomaResp)
	if err != nil {
		logger.Errorf("Inventory.Pick failed: %v", err)
		return r.gomaResp
	}

	r.filepath, err = descriptor.FilePathOf(cmdConfig.GetCmdDescriptor().GetSetup().GetPathType())
	if err != nil {
		logger.Errorf("bad path type in setup %s: %v", cmdConfig.GetCmdDescriptor().GetSelector(), err)
		r.gomaResp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("bad compiler config: %v", err))
		return r.gomaResp
	}

	r.cmdConfig = cmdConfig
	r.cmdFiles = cmdFiles

	r.platform = &rpb.Platform{}
	for _, prop := range cmdConfig.GetRemoteexecPlatform().GetProperties() {
		r.addPlatformProperty(ctx, prop.Name, prop.Value)
	}
	r.allowChroot = cmdConfig.GetRemoteexecPlatform().GetHasNsjail()
	logger.Infof("platform: %s, allowChroot=%t", r.platform, r.allowChroot)
	return nil
}

func (r *request) addPlatformProperty(ctx context.Context, name, value string) {
	for _, p := range r.platform.Properties {
		if p.Name == name {
			p.Value = value
			return
		}
	}
	r.platform.Properties = append(r.platform.Properties, &rpb.Platform_Property{
		Name:  name,
		Value: value,
	})
}

type inputDigestData struct {
	filename string
	digest.Data
}

func (id inputDigestData) String() string {
	return fmt.Sprintf("%s %s", id.Data.String(), id.filename)
}

func changeSymlinkAbsToRel(e merkletree.Entry) (merkletree.Entry, error) {
	dir := filepath.Dir(e.Name)
	if !filepath.IsAbs(dir) {
		return merkletree.Entry{}, fmt.Errorf("absolute symlink path not allowed: %s -> %s", e.Name, e.Target)
	}
	target, err := filepath.Rel(dir, e.Target)
	if err != nil {
		return merkletree.Entry{}, fmt.Errorf("failed to make relative for absolute symlink path: %s in %s -> %s: %v", e.Name, dir, e.Target, err)
	}
	e.Target = target
	return e, nil
}

type gomaInputInterface interface {
	toDigest(context.Context, *gomapb.ExecReq_Input) (digest.Data, error)
	upload(context.Context, []*gomapb.FileBlob) ([]string, error)
}

func uploadInputFiles(ctx context.Context, inputs []*gomapb.ExecReq_Input, gi gomaInputInterface, sema chan struct{}) error {
	count := 0
	size := 0
	batchLimit := 500
	sizeLimit := 10 * 1024 * 1024

	beginOffset := 0
	hashKeys := make([]string, len(inputs))

	eg, ctx := errgroup.WithContext(ctx)

	for i, input := range inputs {
		count++
		size += len(input.Content.Content)

		// Upload a bunch of file blobs if one of the following:
		// - inputs[uploadBegin:i] reached the upload blob count limit
		// - inputs[uploadBegin:i] exceeds the upload blob size limit
		// - we are on the last blob to be uploaded
		if count < batchLimit && size < sizeLimit && i < len(inputs)-1 {
			continue
		}

		inputs := inputs[beginOffset : i+1]
		results := hashKeys[beginOffset : i+1]
		eg.Go(func() error {
			sema <- struct{}{}
			defer func() {
				<-sema
			}()

			contents := make([]*gomapb.FileBlob, len(inputs))
			for i, input := range inputs {
				contents[i] = input.Content
			}

			var hks []string
			var err error
			err = rpc.Retry{}.Do(ctx, func() error {
				hks, err = gi.upload(ctx, contents)
				return err
			})

			if err != nil {
				return fmt.Errorf("setup %s input error: %v", inputs[0].GetFilename(), err)
			}
			if len(hks) != len(contents) {
				return fmt.Errorf("invalid number of hash keys: %d, want %d", len(hks), len(contents))
			}
			for i, hk := range hks {
				input := inputs[i]
				if input.GetHashKey() != hk {
					return fmt.Errorf("hashkey missmatch: embedded input %s %s != %s", input.GetFilename(), input.GetHashKey(), hk)
				}
				results[i] = hk
			}
			return nil
		})
		beginOffset = i + 1
		count = 0
		size = 0
	}

	defer func() {
		maxOutputSize := len(inputs)
		if maxOutputSize > 10 {
			maxOutputSize = 10
		}
		successfulUploadsMsg := make([]string, 0, maxOutputSize+1)
		for i, input := range inputs {
			if len(hashKeys[i]) == 0 {
				continue
			}
			if i == maxOutputSize && i < len(inputs)-1 {
				successfulUploadsMsg = append(successfulUploadsMsg, "...")
				break
			}
			successfulUploadsMsg = append(successfulUploadsMsg, fmt.Sprintf("%s -> %s", input.GetFilename(), hashKeys[i]))
		}
		logger := log.FromContext(ctx)
		logger.Infof("embedded inputs: %v", successfulUploadsMsg)

		numSuccessfulUploads := 0
		for _, hk := range hashKeys {
			if len(hk) > 0 {
				numSuccessfulUploads++
			}
		}
		if numSuccessfulUploads < len(inputs) {
			logger.Errorf("%d file blobs successfully uploaded, out of %d", numSuccessfulUploads, len(inputs))
		}
	}()

	return eg.Wait()
}

type inputFileResult struct {
	missingInput  string
	missingReason string
	file          merkletree.Entry
	uploaded      bool
	err           error
}

func inputFiles(ctx context.Context, inputs []*gomapb.ExecReq_Input, gi gomaInputInterface, rootRel func(string) (string, error), executableInputs map[string]bool, sema chan struct{}) []inputFileResult {
	logger := log.FromContext(ctx)
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]inputFileResult, len(inputs))
	for i, input := range inputs {
		wg.Add(1)
		go func(input *gomapb.ExecReq_Input, result *inputFileResult) {
			defer wg.Done()
			sema <- struct{}{}
			defer func() {
				<-sema
			}()

			fname, err := rootRel(input.GetFilename())
			if err != nil {
				if err == errOutOfRoot {
					logger.Warnf("filename %s: %v", input.GetFilename(), err)
					return
				}
				result.err = fmt.Errorf("input file: %s %v", input.GetFilename(), err)
				return
			}

			data, err := gi.toDigest(ctx, input)
			if err != nil {
				result.missingInput = input.GetFilename()
				result.missingReason = fmt.Sprintf("input: %v", err)
				return
			}
			file := merkletree.Entry{
				Name: fname,
				Data: inputDigestData{
					filename: input.GetFilename(),
					Data:     data,
				},
				IsExecutable: executableInputs[input.GetFilename()],
			}
			result.file = file
			if input.Content == nil {
				return
			}
			result.uploaded = true
		}(input, &results[i])
	}
	wg.Wait()

	return results
}

// newInputTree constructs input tree from req.
// it returns non-nil ExecResp for:
// - missing inputs
// - input root detection failed
func (r *request) newInputTree(ctx context.Context) *gomapb.ExecResp {
	if r.err != nil {
		return nil
	}
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/remoteexec.request.newInputTree")
	defer span.End()
	logger := log.FromContext(ctx)

	inputPaths, err := inputPaths(r.filepath, r.gomaReq, r.cmdFiles[0].Path)
	if err != nil {
		logger.Errorf("bad input: %v", err)
		r.gomaResp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("bad input: %v", err))
		return r.gomaResp
	}
	rootDir, needChroot, err := inputRootDir(r.filepath, inputPaths, r.allowChroot)
	if err != nil {
		logger.Errorf("input root detection failed: %v", err)
		logFileList(logger, "input paths", inputPaths)
		r.gomaResp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("input root detection failed: %v", err))
		return r.gomaResp
	}
	r.tree = merkletree.New(r.filepath, rootDir, r.digestStore)
	r.needChroot = needChroot

	logger.Infof("new input tree cwd:%s root:%s %s", r.gomaReq.GetCwd(), r.tree.RootDir(), r.cmdConfig.GetCmdDescriptor().GetSetup().GetPathType())
	// If toolchain_included is true, r.gomaReq.Input and cmdFiles will contain the same files.
	// To avoid dup, if it's added in r.gomaReq.Input, we don't add it as cmdFiles.
	// While processing r.gomaReq.Input, we handle missing input, so the main routine is in
	// r.gomaReq.Input.

	// path from cwd -> is_executable. Don't confuse "path from cwd" and "path from input root".
	// Everything (except symlink) in ToolchainSpec should be in r.gomaReq.Input.
	// If not and it's necessary to execute, a runtime error (while compile) can happen.
	// e.g. *.so is missing etc.
	toolchainInputs := make(map[string]bool)
	executableInputs := make(map[string]bool)
	if r.gomaReq.GetToolchainIncluded() {
		for _, ts := range r.gomaReq.ToolchainSpecs {
			if ts.GetSymlinkPath() != "" {
				// If toolchain is a symlink, it is not included in r.gomaReq.Input.
				// So, toolchainInputs should not contain it.
				continue
			}
			toolchainInputs[ts.GetPath()] = true
			if ts.GetIsExecutable() {
				executableInputs[ts.GetPath()] = true
			}
		}
	}

	cleanCWD := r.filepath.Clean(r.gomaReq.GetCwd())
	cleanRootDir := r.filepath.Clean(r.tree.RootDir())

	gi := gomaInput{
		gomaFile:    r.f.GomaFile,
		digestCache: r.f.DigestCache,
	}
	results := inputFiles(ctx, r.gomaReq.Input, gi, func(filename string) (string, error) {
		return rootRel(r.filepath, filename, cleanCWD, cleanRootDir)
	}, executableInputs, r.f.FileLookupSema)
	for _, result := range results {
		if result.err != nil {
			r.err = result.err
			return nil
		}
	}

	uploads := make([]*gomapb.ExecReq_Input, 0, len(r.gomaReq.Input))
	for i, input := range r.gomaReq.Input {
		result := &results[i]
		if result.uploaded {
			uploads = append(uploads, input)
		}
	}

	err = uploadInputFiles(ctx, uploads, gi, r.f.FileLookupSema)
	if err != nil {
		r.err = err
		return nil
	}

	var uploaded int
	var files []merkletree.Entry
	var missingInputs []string
	var missingReason []string
	for _, in := range results {
		if in.missingInput != "" {
			missingInputs = append(missingInputs, in.missingInput)
			missingReason = append(missingReason, in.missingReason)
			continue
		}
		files = append(files, in.file)
		if in.uploaded {
			uploaded++
		}
	}
	logger.Infof("upload %d inputs out of %d", uploaded, len(r.gomaReq.Input))
	if len(missingInputs) > 0 {
		r.gomaResp.MissingInput = missingInputs
		r.gomaResp.MissingReason = missingReason
		sortMissing(r.gomaReq.Input, r.gomaResp)
		logFileList(logger, "missing inputs", r.gomaResp.MissingInput)
		return r.gomaResp
	}

	// create wrapper scripts
	err = r.newWrapperScript(ctx, r.cmdConfig, r.cmdFiles[0].Path)
	if err != nil {
		r.err = fmt.Errorf("wrapper script: %v", err)
		return nil
	}

	symAbsOk := r.f.capabilities.GetCacheCapabilities().GetSymlinkAbsolutePathStrategy() == rpb.SymlinkAbsolutePathStrategy_ALLOWED

	for _, f := range r.cmdFiles {
		if _, found := toolchainInputs[f.Path]; found {
			// Must be processed in r.gomaReq.Input. So, skip this.
			// TODO: cmdFiles should be empty instead if toolchain_included = true case?
			continue
		}

		e, err := fileSpecToEntry(ctx, f, r.f.CmdStorage)
		if err != nil {
			r.err = fmt.Errorf("fileSpecToEntry: %v", err)
			return nil
		}
		if !symAbsOk && e.Target != "" && filepath.IsAbs(e.Target) {
			e, err = changeSymlinkAbsToRel(e)
			if err != nil {
				r.err = err
				return nil
			}
		}
		fname, err := rootRel(r.filepath, e.Name, cleanCWD, cleanRootDir)
		if err != nil {
			if err == errOutOfRoot {
				continue
			}
			r.err = fmt.Errorf("command file: %v", err)
			return nil
		}
		e.Name = fname
		files = append(files, e)
	}

	addDirs := func(name string, dirs []string) {
		if r.err != nil {
			return
		}
		for _, d := range dirs {
			rel, err := rootRel(r.filepath, d, cleanCWD, cleanRootDir)
			if err != nil {
				if err == errOutOfRoot {
					logger.Warnf("%s %s: %v", name, d, err)
					continue
				}
				r.err = fmt.Errorf("%s %s: %v", name, d, err)
				return
			}
			files = append(files, merkletree.Entry{
				// directory
				Name: rel,
			})
		}
	}
	// Set up system include and framework paths (b/119072207)
	// -isystem etc can be set for a compile, and non-existence of a directory specified by -isystem may cause compile error even if no file inside the directory is used.
	addDirs("cxx system include path", r.gomaReq.GetCommandSpec().GetCxxSystemIncludePath())
	addDirs("system include path", r.gomaReq.GetCommandSpec().GetSystemIncludePath())
	addDirs("system framework path", r.gomaReq.GetCommandSpec().GetSystemFrameworkPath())

	// prepare output dirs.
	r.outputs = outputs(ctx, r.cmdConfig, r.gomaReq)
	var outDirs []string
	for _, d := range r.outputs {
		outDirs = append(outDirs, r.filepath.Dir(d))
	}
	addDirs("output file", outDirs)
	r.outputDirs = outputDirs(ctx, r.cmdConfig, r.gomaReq)
	addDirs("output dir", r.outputDirs)
	if r.err != nil {
		return nil
	}

	for _, f := range files {
		err = r.tree.Set(f)
		if err != nil {
			r.err = fmt.Errorf("input file: %v: %v", f, err)
			return nil
		}
	}

	root, err := r.tree.Build(ctx)
	if err != nil {
		r.err = err
		return nil
	}
	logger.Infof("input root digest: %v", root)
	r.action.InputRootDigest = root

	return nil
}

type wrapperType int

const (
	wrapperRelocatable wrapperType = iota
	wrapperBindMount
	wrapperNsjailChroot
	wrapperWin
)

func (w wrapperType) String() string {
	switch w {
	case wrapperRelocatable:
		return "wrapper-relocatable"
	case wrapperBindMount:
		return "wrapper-bind-mount"
	case wrapperNsjailChroot:
		return "wrapper-nsjail-chroot"
	case wrapperWin:
		return "wrapper-win"
	default:
		return fmt.Sprintf("wrapper-unknown-%d", int(w))
	}
}

const (
	bindMountWrapperScript = `#!/bin/bash
# run command (i.e. "$@") at the same dir as user.
#  INPUT_ROOT_DIR: expected directory of input root.
#    input root is current directory to run this script.
#    need to mount input root on $INPUT_ROOT_DIR.
#  WORK_DIR: working directory relative to INPUT_ROOT_DIR.
#    command will run at $INPUT_ROOT_DIR/$WORK_DIR.
set -e

# check inputs.
# TODO: report error on other channel than stderr.
if [[ "$INPUT_ROOT_DIR" == "" ]]; then
  echo "ERROR: INPUT_ROOT_DIR is not set" >&2
  exit 1
fi
if [[ "$WORK_DIR" == "" ]]; then
  echo "ERROR: WORK_DIR is not set" >&2
  exit 1
fi
# if container image provides the script, use it instead.
if [[ -x /opt/goma/bin/run-at-dir.sh ]]; then
  exec /opt/goma/bin/run-at-dir.sh "$@"
  echo "ERROR: exec /opt/goma/bin/run-at-dir.sh failed: $?" >&2
  exit 1
fi

mkdir -p "${INPUT_ROOT_DIR}" || true # require root
mount --bind "$(pwd)" "${INPUT_ROOT_DIR}" # require privileged
cd "${INPUT_ROOT_DIR}/${WORK_DIR}"
# TODO: run as normal user, not as root.
# run as nobody will fail with "unable to open output file. permission denied"
"$@"
`

	relocatableWrapperScript = `#!/bin/bash
set -e
if [[ "$WORK_DIR" != "" ]]; then
  cd "${WORK_DIR}"
fi
exec "$@"
`
)

// TODO: put wrapper script in platform container?
func (r *request) newWrapperScript(ctx context.Context, cmdConfig *cmdpb.Config, argv0 string) error {
	logger := log.FromContext(ctx)

	cwd := r.gomaReq.GetCwd()
	cleanCWD := r.filepath.Clean(cwd)
	cleanRootDir := r.filepath.Clean(r.tree.RootDir())
	wd, err := rootRel(r.filepath, cwd, cleanCWD, cleanRootDir)
	if err != nil {
		return fmt.Errorf("bad cwd=%s: %v", cwd, err)
	}
	if wd == "" {
		wd = "."
	}
	envs := []string{fmt.Sprintf("WORK_DIR=%s", wd)}

	// The developer of this program can make multiple wrapper scripts
	// to be used by adding fileDesc instances to `files`.
	// However, only the first one is called in the command line.
	// The other scripts should be called from the first wrapper script
	// if needed.
	type fileDesc struct {
		name         string
		data         digest.Data
		isExecutable bool
	}
	var files []fileDesc

	args := buildArgs(ctx, cmdConfig, argv0, r.gomaReq)
	// TODO: only allow whitelisted envs.

	wt := wrapperRelocatable
	pathType := cmdConfig.GetCmdDescriptor().GetSetup().GetPathType()
	switch pathType {
	case cmdpb.CmdDescriptor_POSIX:
		if r.needChroot {
			wt = wrapperNsjailChroot
		} else {
			err = relocatableReq(ctx, cmdConfig, r.filepath, r.gomaReq.Arg, r.gomaReq.Env)
			if err != nil {
				wt = wrapperBindMount
				logger.Infof("non relocatable: %v", err)
			}
		}
	case cmdpb.CmdDescriptor_WINDOWS:
		wt = wrapperWin
	default:
		return fmt.Errorf("bad path type: %v", pathType)
	}

	const posixWrapperName = "run.sh"
	switch wt {
	case wrapperNsjailChroot:
		logger.Infof("run with nsjail chroot")
		// needed for bind mount.
		r.addPlatformProperty(ctx, "dockerPrivileged", "true")
		// needed for chroot command and mount command.
		r.addPlatformProperty(ctx, "dockerRunAsRoot", "true")
		nsjailCfg := nsjailConfig(cwd, r.filepath, r.gomaReq.GetToolchainSpecs(), r.gomaReq.Env)
		files = []fileDesc{
			{
				name:         posixWrapperName,
				data:         digest.Bytes("nsjail-run-wrapper-script", []byte(nsjailRunWrapperScript)),
				isExecutable: true,
			},
			{
				name: "nsjail.cfg",
				data: digest.Bytes("nsjail-config-file", []byte(nsjailCfg)),
			},
		}
	case wrapperBindMount:
		logger.Infof("run with bind mount")
		envs = append(envs, fmt.Sprintf("INPUT_ROOT_DIR=%s", r.tree.RootDir()))
		// needed for nsjail (bind mount).
		// https://cloud.google.com/remote-build-execution/docs/remote-execution-properties#container_properties
		// dockerAddCapabilities=SYS_ADMIN is not sufficient.
		// need -security-opt=apparmor:unconfined too?
		// https://github.com/moby/moby/issues/16429
		r.addPlatformProperty(ctx, "dockerPrivileged", "true")
		// needed for mkdir $INPUT_ROOT_DIR
		r.addPlatformProperty(ctx, "dockerRunAsRoot", "true")
		for _, e := range r.gomaReq.Env {
			envs = append(envs, e)
		}
		files = []fileDesc{
			{
				name:         posixWrapperName,
				data:         digest.Bytes("nsjail-bind-mount-wrapper-script", []byte(bindMountWrapperScript)),
				isExecutable: true,
			},
		}
	case wrapperRelocatable:
		logger.Infof("run with chdir: relocatable")
		for _, e := range r.gomaReq.Env {
			if strings.HasPrefix(e, "PWD=") {
				// PWD is usually absolute path.
				// if relocatable, then we should remove
				// PWD environment variable.
				continue
			}
			envs = append(envs, e)
		}
		files = []fileDesc{
			{
				name:         posixWrapperName,
				data:         digest.Bytes("relocatable-wrapper-script", []byte(relocatableWrapperScript)),
				isExecutable: true,
			},
		}
	case wrapperWin:
		logger.Infof("run on win")
		wn, data, err := wrapperForWindows(ctx)
		if err != nil {
			return err
		}
		files = []fileDesc{
			{
				name:         wn,
				data:         data,
				isExecutable: true,
			},
		}
	default:
		return fmt.Errorf("bad wrapper type: %v", wt)
	}

	// Only the first one is called in the command line via storing
	// `wrapperPath` in `r.args` later.
	wrapperPath := ""
	for i, w := range files {
		wp, err := rootRel(r.filepath, w.name, cleanCWD, cleanRootDir)
		if err != nil {
			return err
		}

		logger.Infof("file (%d) %s => %v", i, wp, w.data.Digest())
		r.tree.Set(merkletree.Entry{
			Name:         wp,
			Data:         w.data,
			IsExecutable: w.isExecutable,
		})
		if wrapperPath == "" {
			wrapperPath = wp
		}
	}

	r.envs = envs

	// if a wrapper exists in cwd, `wrapper` does not have a directory name.
	// It cannot be callable on POSIX because POSIX do not contain "." in
	// its PATH.
	if wrapperPath == posixWrapperName {
		wrapperPath = "./" + posixWrapperName
	}
	r.args = append([]string{wrapperPath}, args...)

	err = stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(wrapperTypeKey, wt.String())}, wrapperCount.M(1))
	if err != nil {
		logger.Errorf("record wrapper-count %s: %v", wt, err)
	}
	return nil
}

// TODO: refactor with exec/clang.go, exec/clangcl.go?

// buildArgs builds args in RBE from arg0 and req, respecting cmdConfig.
func buildArgs(ctx context.Context, cmdConfig *cmdpb.Config, arg0 string, req *gomapb.ExecReq) []string {
	// TODO: need compiler specific handling?
	args := append([]string{arg0}, req.Arg[1:]...)
	if cmdConfig.GetCmdDescriptor().GetCross().GetClangNeedTarget() {
		args = addTargetIfNotExist(args, req.GetCommandSpec().GetTarget())
	}
	return args
}

// add target option to args if args doesn't already have target option.
func addTargetIfNotExist(args []string, target string) []string {
	// no need to add -target if arg already have it.
	for _, arg := range args {
		if arg == "-target" || strings.HasPrefix(arg, "--target=") {
			return args
		}
	}
	// https://clang.llvm.org/docs/CrossCompilation.html says
	// `-target <triple>`, but clang --help shows
	//  --target=<value>        Generate code for the given target
	return append(args, fmt.Sprintf("--target=%s", target))
}

// relocatableReq checks args, envs is relocatable, respecting cmdConfig.
func relocatableReq(ctx context.Context, cmdConfig *cmdpb.Config, filepath clientFilePath, args, envs []string) error {
	switch name := cmdConfig.GetCmdDescriptor().GetSelector().GetName(); name {
	case "gcc", "g++", "clang", "clang++":
		return gccRelocatableReq(filepath, args, envs)
	case "clang-cl":
		return clangclRelocatableReq(args, envs)
	case "javac":
		// Currently, javac in Chromium is fully relocatable. Simpler just to
		// support only the relocatable case and let it fail if the client passed
		// in invalid absolute paths.
		return nil
	default:
		// "cl.exe", "clang-tidy"
		return fmt.Errorf("no relocatable check for %s", name)
	}
}

// outputs gets output filenames from gomaReq.
// If either expected_output_files or expected_output_dirs is specified,
// expected_output_files is used.
// Otherwise, it's calculated from args.
func outputs(ctx context.Context, cmdConfig *cmdpb.Config, gomaReq *gomapb.ExecReq) []string {
	if len(gomaReq.ExpectedOutputFiles) > 0 || len(gomaReq.ExpectedOutputDirs) > 0 {
		return gomaReq.GetExpectedOutputFiles()
	}

	args := gomaReq.Arg
	switch name := cmdConfig.GetCmdDescriptor().GetSelector().GetName(); name {
	case "gcc", "g++", "clang", "clang++":
		return gccOutputs(args)
	case "clang-cl":
		return clangclOutputs(args)
	default:
		// "cl.exe", "javac", "clang-tidy"
		return nil
	}
}

// outputDirs gets output dirnames from gomaReq.
// If either expected_output_files or expected_output_dirs is specified,
// expected_output_dirs is used.
// Otherwise, it's calculated from args.
func outputDirs(ctx context.Context, cmdConfig *cmdpb.Config, gomaReq *gomapb.ExecReq) []string {
	if len(gomaReq.ExpectedOutputFiles) > 0 || len(gomaReq.ExpectedOutputDirs) > 0 {
		return gomaReq.GetExpectedOutputDirs()
	}

	args := gomaReq.Arg
	switch cmdConfig.GetCmdDescriptor().GetSelector().GetName() {
	case "javac":
		return javacOutputDirs(args)
	default:
		return nil
	}
}

func (r *request) setupNewAction(ctx context.Context) {
	if r.err != nil {
		return
	}
	command, err := r.newCommand(ctx)
	if err != nil {
		r.err = err
		return
	}

	// we'll run  wrapper script that chdir, so don't set chdir here.
	// see newWrapperScript.
	// TODO: set command.WorkingDirectory
	data, err := digest.Proto(command)
	if err != nil {
		r.err = err
		return
	}
	logger := log.FromContext(ctx)
	logger.Infof("command digest: %v", data.Digest())

	r.digestStore.Set(data)
	r.action.CommandDigest = data.Digest()

	data, err = digest.Proto(r.action)
	if err != nil {
		r.err = err
		return
	}
	r.digestStore.Set(data)
	logger.Infof("action digest: %v %s", data.Digest(), r.action)
	r.actionDigest = data.Digest()
}

func (r *request) newCommand(ctx context.Context) (*rpb.Command, error) {
	logger := log.FromContext(ctx)

	envVars := createEnvVars(ctx, r.envs)
	sort.Slice(r.platform.Properties, func(i, j int) bool {
		return r.platform.Properties[i].Name < r.platform.Properties[j].Name
	})
	command := &rpb.Command{
		Arguments:            r.args,
		EnvironmentVariables: envVars,
		Platform:             r.platform,
	}

	logger.Debugf("setup for outputs: %v", r.outputs)
	cleanCWD := r.filepath.Clean(r.gomaReq.GetCwd())
	cleanRootDir := r.filepath.Clean(r.tree.RootDir())
	// set output files from command line flags.
	for _, output := range r.outputs {
		rel, err := rootRel(r.filepath, output, cleanCWD, cleanRootDir)
		if err != nil {
			return nil, fmt.Errorf("output %s: %v", output, err)
		}
		command.OutputFiles = append(command.OutputFiles, rel)
	}
	sort.Strings(command.OutputFiles)

	logger.Debugf("setup for output dirs: %v", r.outputDirs)
	// set output dirs from command line flags.
	for _, output := range r.outputDirs {
		rel, err := rootRel(r.filepath, output, cleanCWD, cleanRootDir)
		if err != nil {
			return nil, fmt.Errorf("output dir %s: %v", output, err)
		}
		command.OutputDirectories = append(command.OutputDirectories, rel)
	}
	sort.Strings(command.OutputDirectories)

	return command, nil
}

func (r *request) checkCache(ctx context.Context) (*rpb.ActionResult, bool) {
	if r.err != nil {
		// no need to ask to execute.
		return nil, true
	}
	logger := log.FromContext(ctx)
	if skipCacheLookup(r.gomaReq) {
		logger.Infof("store_only; skip cache lookup")
		return nil, false
	}
	resp, err := r.client.Cache().GetActionResult(ctx, &rpb.GetActionResultRequest{
		InstanceName: r.instanceName(),
		ActionDigest: r.actionDigest,
	})
	if err != nil {
		switch status.Code(err) {
		case codes.NotFound:
			logger.Infof("no cached action %v: %v", r.actionDigest, err)
		case codes.Unavailable, codes.Canceled, codes.Aborted:
			logger.Warnf("get action result %v: %v", r.actionDigest, err)
		default:
			logger.Errorf("get action result %v: %v", r.actionDigest, err)
		}
		return nil, false
	}
	return resp, true
}

func (r *request) missingBlobs(ctx context.Context) ([]*rpb.Digest, error) {
	if r.err != nil {
		return nil, r.err
	}
	var blobs []*rpb.Digest
	err := rpc.Retry{}.Do(ctx, func() error {
		var err error
		blobs, err = r.cas.Missing(ctx, r.instanceName(), r.digestStore.List())
		return fixRBEInternalError(err)
	})
	if err != nil {
		r.err = err
		return nil, err
	}
	return blobs, nil
}

func inputForDigest(ds *digest.Store, d *rpb.Digest) (string, error) {
	src, ok := ds.GetSource(d)
	if !ok {
		return "", fmt.Errorf("not found for %s", d)
	}
	idd, ok := src.(inputDigestData)
	if !ok {
		return "", fmt.Errorf("not input file for %s", d)
	}
	return idd.filename, nil
}

type byInputFilenames struct {
	order map[string]int
	resp  *gomapb.ExecResp
}

func (b byInputFilenames) Len() int { return len(b.resp.MissingInput) }
func (b byInputFilenames) Swap(i, j int) {
	b.resp.MissingInput[i], b.resp.MissingInput[j] = b.resp.MissingInput[j], b.resp.MissingInput[i]
	b.resp.MissingReason[i], b.resp.MissingReason[j] = b.resp.MissingReason[j], b.resp.MissingReason[i]
}

func (b byInputFilenames) Less(i, j int) bool {
	io := b.order[b.resp.MissingInput[i]]
	jo := b.order[b.resp.MissingInput[j]]
	return io < jo
}

func sortMissing(inputs []*gomapb.ExecReq_Input, resp *gomapb.ExecResp) {
	m := make(map[string]int)
	for i, input := range inputs {
		m[input.GetFilename()] = i
	}
	sort.Sort(byInputFilenames{
		order: m,
		resp:  resp,
	})
}

func logFileList(logger log.Logger, msg string, files []string) {
	s := fmt.Sprintf("%q", files)
	const logLineThreshold = 95 * 1024
	if len(s) < logLineThreshold {
		logger.Infof("%s %s", msg, s)
		return
	}
	logger.Warnf("too many %s %d", msg, len(files))
	var b strings.Builder
	var i int
	for len(files) > 0 {
		if b.Len() > 0 {
			fmt.Fprintf(&b, " ")
		}
		s, files = files[0], files[1:]
		fmt.Fprintf(&b, "%q", s)
		if b.Len() > logLineThreshold {
			logger.Infof("%s %d: [%s]", msg, i, b)
			i++
			b.Reset()
		}
	}
	if b.Len() > 0 {
		logger.Infof("%s %d: [%s]", msg, i, b)
	}
}

func (r *request) uploadBlobs(ctx context.Context, blobs []*rpb.Digest) (*gomapb.ExecResp, error) {
	if r.err != nil {
		return nil, r.err
	}
	err := r.cas.Upload(ctx, r.instanceName(), r.f.CASBlobLookupSema, blobs...)
	if err != nil {
		if missing, ok := err.(cas.MissingError); ok {
			logger := log.FromContext(ctx)
			logger.Infof("failed to upload blobs %s", missing.Blobs)
			var missingInputs []string
			var missingReason []string
			for _, b := range missing.Blobs {
				fname, err := inputForDigest(r.digestStore, b.Digest)
				if err != nil {
					logger.Warnf("unknown input for %s: %v", b.Digest, err)
					continue
				}
				missingInputs = append(missingInputs, fname)
				missingReason = append(missingReason, b.Err.Error())
			}
			if len(missingInputs) > 0 {
				r.gomaResp.MissingInput = missingInputs
				r.gomaResp.MissingReason = missingReason
				sortMissing(r.gomaReq.Input, r.gomaResp)
				logFileList(logger, "missing inputs", r.gomaResp.MissingInput)
				return r.gomaResp, nil
			}
			// failed to upload non-input, so no need to report
			// missing input to users.
			// handle it as grpc error.
		}
		r.err = err
	}
	return nil, err
}

func (r *request) executeAction(ctx context.Context) (*rpb.ExecuteResponse, error) {
	if r.err != nil {
		return nil, r.Err()
	}
	_, resp, err := ExecuteAndWait(ctx, r.client, &rpb.ExecuteRequest{
		InstanceName:    r.instanceName(),
		SkipCacheLookup: skipCacheLookup(r.gomaReq),
		ActionDigest:    r.actionDigest,
		// ExecutionPolicy
		// ResultsCachePolicy
	})
	if err != nil {
		r.err = err
		return nil, r.Err()
	}
	return resp, nil
}

func timestampSub(ctx context.Context, t1, t2 *tspb.Timestamp) time.Duration {
	logger := log.FromContext(ctx)
	time1, err := ptypes.Timestamp(t1)
	if err != nil {
		logger.Errorf("%s: %v", t1, err)
		return 0
	}
	time2, err := ptypes.Timestamp(t2)
	if err != nil {
		logger.Errorf("%s: %v", t2, err)
		return 0
	}
	return time1.Sub(time2)
}

func (r *request) newResp(ctx context.Context, eresp *rpb.ExecuteResponse, cached bool) (*gomapb.ExecResp, error) {
	logger := log.FromContext(ctx)
	if r.err != nil {
		logger.Warnf("error during exec: %v", r.err)
		return nil, r.Err()
	}
	logger.Debugf("response %v cached=%t", eresp, cached)
	r.gomaResp.CacheKey = proto.String(r.actionDigest.String())
	switch {
	case eresp.CachedResult:
		r.gomaResp.CacheHit = gomapb.ExecResp_STORAGE_CACHE.Enum()
	case cached:
		r.gomaResp.CacheHit = gomapb.ExecResp_MEM_CACHE.Enum()
	default:
		r.gomaResp.CacheHit = gomapb.ExecResp_NO_CACHE.Enum()
	}
	if st := eresp.GetStatus(); st.GetCode() != 0 {
		logger.Errorf("execute status error: %v", st)
		s := status.FromProto(st)
		r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("Execute error: %s", s.Code()))
		logger.Errorf("resp %v", r.gomaResp)
		return r.gomaResp, nil
	}
	if eresp.Result == nil {
		r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, "unexpected response message")
		logger.Errorf("resp %v", r.gomaResp)
		return r.gomaResp, nil
	}
	md := eresp.Result.GetExecutionMetadata()
	logger.Infof("exit=%d cache=%s : exec on %q queue=%s worker=%s input=%s exec=%s output=%s",
		eresp.Result.GetExitCode(),
		r.gomaResp.GetCacheHit(),
		md.GetWorker(),
		timestampSub(ctx, md.GetWorkerStartTimestamp(), md.GetQueuedTimestamp()),
		timestampSub(ctx, md.GetWorkerCompletedTimestamp(), md.GetWorkerStartTimestamp()),
		timestampSub(ctx, md.GetInputFetchCompletedTimestamp(), md.GetInputFetchStartTimestamp()),
		timestampSub(ctx, md.GetExecutionCompletedTimestamp(), md.GetExecutionStartTimestamp()),
		timestampSub(ctx, md.GetOutputUploadCompletedTimestamp(), md.GetOutputUploadStartTimestamp()))
	r.gomaResp.ExecutionStats = &gomapb.ExecutionStats{
		ExecutionStartTimestamp:     md.GetExecutionStartTimestamp(),
		ExecutionCompletedTimestamp: md.GetExecutionCompletedTimestamp(),
	}
	gout := gomaOutput{
		gomaResp: r.gomaResp,
		bs:       r.client.ByteStream(),
		instance: r.instanceName(),
		gomaFile: r.f.GomaFile,
	}
	// TODO: gomaOutput should return err for codes.Unauthenticated,
	// instead of setting ErrorMessage in r.gomaResp,
	// so it returns to caller (i.e. frontend), and retry with new
	// refreshed oauth2 access.
	// token.
	gout.stdoutData(ctx, eresp)
	gout.stderrData(ctx, eresp)

	if len(r.gomaResp.Result.StdoutBuffer) > 0 {
		// docker failure would be error of goma server, not users.
		// so make it internal error, rather than command execution error.
		// http://b/80272874
		const dockerErrorResponse = "docker: Error response from daemon: oci runtime error:"
		if eresp.Result.ExitCode == 127 &&
			bytes.Contains(r.gomaResp.Result.StdoutBuffer, []byte(dockerErrorResponse)) {
			logger.Errorf("docker error response %s", shortLogMsg(r.gomaResp.Result.StdoutBuffer))
			return r.gomaResp, status.Errorf(codes.Internal, "docker error: %s", string(r.gomaResp.Result.StdoutBuffer))
		}

		if eresp.Result.ExitCode != 0 {
			logLLVMError(logger, "stdout", r.gomaResp.Result.StdoutBuffer)
		}
		logger.Infof("stdout %s", shortLogMsg(r.gomaResp.Result.StdoutBuffer))
	}
	if len(r.gomaResp.Result.StderrBuffer) > 0 {
		if eresp.Result.ExitCode != 0 {
			logLLVMError(logger, "stderr", r.gomaResp.Result.StderrBuffer)
		}
		logger.Infof("stderr %s", shortLogMsg(r.gomaResp.Result.StderrBuffer))
	}

	for _, output := range eresp.Result.OutputFiles {
		if r.err != nil {
			break
		}
		// output.Path should not be absolute, but relative to root dir.
		// convert it to fname, which is cwd relative.
		fname, err := r.filepath.Rel(r.gomaReq.GetCwd(), r.filepath.Join(r.tree.RootDir(), output.Path))
		if err != nil {
			r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("output path %s: %v", output.Path, err))
			continue
		}
		gout.outputFile(ctx, fname, output)
	}
	for _, output := range eresp.Result.OutputDirectories {
		if r.err != nil {
			break
		}
		// output.Path should not be absolute, but relative to root dir.
		// convert it to fname, which is cwd relative.
		fname, err := r.filepath.Rel(r.gomaReq.GetCwd(), r.filepath.Join(r.tree.RootDir(), output.Path))
		if err != nil {
			r.gomaResp.ErrorMessage = append(r.gomaResp.ErrorMessage, fmt.Sprintf("output path %s: %v", output.Path, err))
			continue
		}
		gout.outputDirectory(ctx, r.filepath, fname, output, r.f.OutputFileSema)
	}
	if len(r.gomaResp.ErrorMessage) == 0 {
		r.gomaResp.Result.ExitStatus = proto.Int32(eresp.Result.ExitCode)
	}

	sizeLimit := exec.DefaultMaxRespMsgSize
	respSize := proto.Size(r.gomaResp)
	if respSize > sizeLimit {
		logger.Infof("gomaResp size=%d, limit=%d, using FileService for larger blobs.", respSize, sizeLimit)
		if err := gout.reduceRespSize(ctx, sizeLimit, r.f.OutputFileSema); err != nil {
			// Don't need to append any error messages to `r.gomaResp` because it won't be sent.
			return nil, fmt.Errorf("failed to reduce resp size below limit=%d, %d -> %d: %v", sizeLimit, respSize, proto.Size(gout.gomaResp), err)
		}
		logger.Infof("gomaResp size reduced %d -> %d", respSize, proto.Size(gout.gomaResp))
	}

	return r.gomaResp, r.Err()
}

func shortLogMsg(msg []byte) string {
	if len(msg) <= 1024 {
		return string(msg)
	}
	var b strings.Builder
	b.Write(msg[:512])
	fmt.Fprint(&b, "...")
	b.Write(msg[len(msg)-512:])
	return b.String()
}

// logLLVMError records LLVM ERROR.
// http://b/145177862
func logLLVMError(logger log.Logger, id string, msg []byte) {
	llvmErrorMsg, ok := extractLLVMError(msg)
	if !ok {
		return
	}
	logger.Errorf("%s: %s", id, llvmErrorMsg)
}

func extractLLVMError(msg []byte) ([]byte, bool) {
	const llvmError = "LLVM ERROR:"
	i := bytes.Index(msg, []byte(llvmError))
	if i < 0 {
		return nil, false
	}
	llvmErrorMsg := msg[i:]
	i = bytes.IndexAny(llvmErrorMsg, "\r\n")
	if i >= 0 {
		llvmErrorMsg = llvmErrorMsg[:i]
	}
	return llvmErrorMsg, true
}
