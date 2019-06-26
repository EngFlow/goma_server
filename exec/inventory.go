// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package exec

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"

	"go.chromium.org/goma/server/command/descriptor"
	"go.chromium.org/goma/server/command/normalizer"
	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
	cmdpb "go.chromium.org/goma/server/proto/command"
)

var (
	toolchainSelects = stats.Int64(
		"go.chromium.org/goma/server/exec.toolchain-selects",
		"Toolchain selection",
		stats.UnitDimensionless)

	selectorKey = tag.MustNewKey("selector")
	resultKey   = tag.MustNewKey("result")

	// DefaultToolchainViews are the default views provided by this package.
	// You need to register the view for data to actually be collected.
	DefaultToolchainViews = []*view.View{
		{
			Description: `counts toolchain selection. result is "used", "found", "requested" or "missed"`,
			TagKeys: []tag.Key{
				selectorKey,
				resultKey,
			},
			Measure:     toolchainSelects,
			Aggregation: view.Count(),
		},
	}
)

type resultValue string

const (
	// toolchain is requested, but not registered.
	resultMissed resultValue = "missed"

	// toolchain (subprogram) is requested, but not checked
	// because command missed.
	resultRequested resultValue = "requested"

	// toolchain is requested and registered, but not used
	// because some other toolchain in the same request missed.
	resultFound resultValue = "found"

	// toolchain is requested and used.
	resultUsed resultValue = "used"
)

func recordToolchainSelect(ctx context.Context, s selector, result resultValue) error {
	// selector string can be too long, more than tag value limit
	// (255 ASCII characters).
	// http://b/115441117
	var buf strings.Builder
	fmt.Fprintf(&buf, "n:%s", s.Name)
	fmt.Fprintf(&buf, " t:%s", s.Target)
	fmt.Fprintf(&buf, " b:%s", s.BinaryHash)
	fmt.Fprintf(&buf, " v:%s", s.Version)
	sval := buf.String()
	if len(sval) > 255 {
		sval = sval[:255]
	}
	ctx, err := tag.New(ctx,
		tag.Upsert(selectorKey, sval),
		tag.Upsert(resultKey, string(result)))
	if err != nil {
		return err
	}
	stats.Record(ctx, toolchainSelects.M(1))
	return nil
}

// Inventory holds available command configs.
type Inventory struct {
	mu        sync.RWMutex
	versionID string
	// map from selector -> slice of addresses.
	addrs map[selector][]string
	// map from address -> selector -> config.
	configs map[string]map[selector]*cmdpb.Config
	// config for arbitrary toolchain support.
	platformConfigs []*platformConfig
}

type selector struct {
	Name       string
	Version    string
	Target     string
	BinaryHash string
}

func fromSelectorProto(s *cmdpb.Selector) selector {
	return selector{
		Name:       s.Name,
		Version:    s.Version,
		Target:     s.Target,
		BinaryHash: s.BinaryHash,
	}
}

func (s selector) Proto() *cmdpb.Selector {
	return &cmdpb.Selector{
		Name:       s.Name,
		Version:    s.Version,
		Target:     s.Target,
		BinaryHash: s.BinaryHash,
	}
}

func (s selector) String() string {
	return s.Proto().String()
}

type byName []selector

func (s byName) Len() int      { return len(s) }
func (s byName) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byName) Less(i, j int) bool {
	return s[i].String() < s[j].String()
}

// platformConfig is for arbitrary toolchain support.
type platformConfig struct {
	dimensionSet       map[string]bool
	remoteexecPlatform *cmdpb.RemoteexecPlatform
}

// Configure sets config in the inventory.
func (in *Inventory) Configure(ctx context.Context, cfgs *cmdpb.ConfigResp) error {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/exec.Service.Configure")
	defer span.End()
	logger := log.FromContext(ctx)
	newAddrs := make(map[selector][]string)
	newConfigs := make(map[string]map[selector]*cmdpb.Config)
	var newPlatformConfigs []*platformConfig
	for _, cfg := range cfgs.Configs {
		// If RemoteexecPlatform exists but CmdDescriptor does not exists,
		// this config is for arbitrary toolchain support.
		// TODO: Split this from ConfigResp.Configs?
		if cfg.CmdDescriptor == nil && cfg.RemoteexecPlatform != nil {
			dimensionSet := make(map[string]bool)
			for _, d := range cfg.GetDimensions() {
				dimensionSet[d] = true
			}
			newPlatformConfigs = append(newPlatformConfigs, &platformConfig{
				dimensionSet:       dimensionSet,
				remoteexecPlatform: cfg.GetRemoteexecPlatform(),
			})
			logger.Infof("configure platform config: %v", cfg)
			continue
		}

		if cfg.Target == nil {
			logger.Warnf("no target in %s", cfg)
			continue
		}
		if cfg.Target.Addr == "" {
			logger.Warnf("no target address in %s", cfg)
			continue
		}
		if cfg.CmdDescriptor == nil || cfg.CmdDescriptor.Selector == nil {
			logger.Warnf("no cmd descriptor in %s", cfg)
			continue
		}
		selpb, err := normalizer.Selector(*cfg.CmdDescriptor.Selector)
		if err != nil {
			logger.Errorf("failed to normalize selector in %s", cfg)
			continue
		}
		sel := fromSelectorProto(&selpb)
		if cfg.CmdDescriptor.GetSetup().GetPathType() == cmdpb.CmdDescriptor_UNKNOWN_PATH_TYPE {
			logger.Errorf("unknown path type in %s %s", sel, cfg)
			continue
		}
		addr := cfg.Target.Addr
		newAddrs[sel] = append(newAddrs[sel], addr)
		m, ok := newConfigs[addr]
		if !ok {
			newConfigs[addr] = make(map[selector]*cmdpb.Config)
			m = newConfigs[addr]
		}
		m[sel] = cfg
		logger.Infof("configure %s: %s", sel, addr)
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	in.versionID = cfgs.VersionId
	in.addrs = newAddrs
	in.configs = newConfigs
	in.platformConfigs = newPlatformConfigs
	if len(in.configs) == 0 && len(in.platformConfigs) == 0 {
		return fmt.Errorf("no available config in %s", cfgs.VersionId)
	}
	return nil
}

func (in *Inventory) VersionID() string {
	in.mu.RLock()
	defer in.mu.RUnlock()
	return in.versionID
}

func (in *Inventory) status() (string, []*cmdpb.Config) {
	in.mu.RLock()
	defer in.mu.RUnlock()
	// sort by addr
	var addrs []string
	for a := range in.configs {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)

	var resp []*cmdpb.Config
	// sort by selector
	for _, a := range addrs {
		var sels []selector
		m := in.configs[a]
		for sel := range m {
			sels = append(sels, sel)
		}
		sort.Sort(byName(sels))
		for _, sel := range sels {
			resp = append(resp, proto.Clone(m[sel]).(*cmdpb.Config))
		}
	}
	return in.versionID, resp
}

// pickCmd takes selectors of compiler and subprograms, and returns configs of
// the best cmd_server that has both compiler and subprograms.
// First, it find out cmd_server that has both selectors of compiler and
// subprograms. (Step 1. and Step 2.)
// Then, it picks cmd_server whose compiler's build time is latest. (Step 3.)
func (in *Inventory) pickCmd(ctx context.Context, cmdSel selector, subprogSels []selector) (*cmdpb.Config, map[selector]*cmdpb.Config, error) {
	logger := log.FromContext(ctx)
	in.mu.RLock()
	defer in.mu.RUnlock()

	record := func(ctx context.Context, s selector, result resultValue) {
		err := recordToolchainSelect(ctx, s, result)
		if err != nil {
			logger.Fatalf("failed to record stats: %s=%s", s, result)
		}
	}

	// 1. command spec selector -> addresses
	addrs, ok := in.addrs[cmdSel]
	if !ok {
		record(ctx, cmdSel, resultMissed)
		for _, s := range subprogSels {
			record(ctx, s, resultRequested)
		}
		return nil, nil, fmt.Errorf("no compiler for %v", cmdSel)
	}

	// 2. choose configs that has all subprograms
	var ccfgs []*cmdpb.Config
	subprogResult := make(map[selector]resultValue)
	for _, s := range subprogSels {
		subprogResult[s] = resultMissed
	}
Loop:
	for _, a := range addrs {
		m, ok := in.configs[a]
		if !ok {
			logger.Errorf("unknown address (%s) is given.", a)
			continue
		}
		for _, s := range subprogSels {
			if _, ok := m[s]; !ok {
				logger.Infof("cfg for %v is not registered in %s.", s, a)
				continue Loop
			}
			subprogResult[s] = resultFound
		}
		cfg, ok := m[cmdSel]
		if !ok {
			logger.Errorf("cfg for %v is not registered. possibly configs broken.", cmdSel)
			continue
		}
		ccfgs = append(ccfgs, cfg)
	}
	if len(ccfgs) == 0 {
		record(ctx, cmdSel, resultFound)
		for s, r := range subprogResult {
			record(ctx, s, r)
		}
		return nil, nil, fmt.Errorf("no matching backend found for %v", cmdSel)
	}

	// 3. choose the latest compiler config.
	sort.Slice(ccfgs, func(i, j int) bool {
		ti, err := ptypes.Timestamp(ccfgs[i].GetBuildInfo().GetTimestamp())
		if err != nil {
			logger.Warnf("strange timestamp: %v", err)
			ti = time.Unix(0, 0)
		}
		tj, err := ptypes.Timestamp(ccfgs[j].GetBuildInfo().GetTimestamp())
		if err != nil {
			logger.Warnf("strange timestamp: %v", err)
			tj = time.Unix(0, 0)
		}
		return ti.Before(tj)
	})
	ccfg := ccfgs[len(ccfgs)-1]

	record(ctx, cmdSel, resultUsed)
	for _, s := range subprogSels {
		record(ctx, s, resultUsed)
	}
	return ccfg, in.configs[ccfg.Target.Addr], nil
}

// Pick picks command and subprograms requested in req, and
// returns config, selector and commands' FileSpec.
// It also update resp.Result about compiler selection.
func (in *Inventory) Pick(ctx context.Context, req *gomapb.ExecReq, resp *gomapb.ExecResp) (*cmdpb.Config, []*cmdpb.FileSpec, error) {
	ctx, span := trace.StartSpan(ctx, "go.chromium.org/goma/server/exec.Service.pick")
	defer span.End()
	logger := log.FromContext(ctx)

	if req.GetToolchainIncluded() {
		// If toolchain is included in ExecReq, pick from ExecReq.
		return in.pickFromExecReq(ctx, req, resp)
	}

	cmdSel, cmdPath, err := fromCommandSpec(req.GetCommandSpec())
	if err != nil {
		resp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		return nil, nil, fmt.Errorf("normalize %v: %v", req.GetCommandSpec(), err)
	}
	span.AddAttributes(
		trace.StringAttribute("command_spec", req.GetCommandSpec().String()),
		trace.StringAttribute("selector", cmdSel.String()),
	)

	var sSels []selector
	path2sel := make(map[string]selector)
	for _, sp := range req.GetSubprogram() {
		ss := fromSubprogramSpec(sp)
		if _, found := path2sel[sp.GetPath()]; found {
			logger.Warnf("subprogram duplicated? skipped: path=%s", sp.GetPath())
			continue
		} else {
			path2sel[sp.GetPath()] = ss
		}
		sSels = append(sSels, ss)
		span.AddAttributes(trace.StringAttribute("subprog:"+sp.GetPath(), ss.String()))
	}

	resp.Result = initResult(req)
	cfg, sels, err := in.pickCmd(ctx, cmdSel, sSels)
	if err != nil {
		resp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		return nil, nil, fmt.Errorf("pick %v: %v", cmdSel, err)
	}
	logger.Infof("pick command %s => %s", cmdPath, cfg.GetCmdDescriptor().GetSelector())
	subprogSetups := make(map[string]*cmdpb.CmdDescriptor_Setup)
	for path, ss := range path2sel {
		logger.Infof("pick subprog %s => %s", path, ss)
		scfg := proto.Clone(sels[ss]).(*cmdpb.Config)
		subprogSetups[path] = scfg.CmdDescriptor.Setup
	}
	setPicked(resp.Result, cfg, path2sel)

	cmdFiles, err := descriptor.RelocateCmd(cmdPath, cfg.CmdDescriptor.Setup, subprogSetups)
	if err != nil {
		resp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		return cfg, nil, fmt.Errorf("relocate %v: %v", cmdSel, err)
	}

	return cfg, cmdFiles, nil
}

func pathTypeFromPathStyle(pathStyle gomapb.RequesterInfo_PathStyle) cmdpb.CmdDescriptor_PathType {
	switch pathStyle {
	case gomapb.RequesterInfo_UNKNOWN_STYLE:
		return cmdpb.CmdDescriptor_UNKNOWN_PATH_TYPE
	case gomapb.RequesterInfo_POSIX_STYLE:
		return cmdpb.CmdDescriptor_POSIX
	case gomapb.RequesterInfo_WINDOWS_STYLE:
		return cmdpb.CmdDescriptor_WINDOWS
	}

	return cmdpb.CmdDescriptor_UNKNOWN_PATH_TYPE
}

// Returns true if all `dimensions` exist in `platformDimensions`.
func matchDimensions(dimensions []string, platformDimensions map[string]bool) bool {
	for _, d := range dimensions {
		if !platformDimensions[d] {
			return false
		}
	}

	// everything matched.
	return true
}

// toolchainSpecToFileSpec converts ToolchainSpec to FileSpec.
func toolchainSpecToFileSpec(ts *gomapb.ToolchainSpec) *cmdpb.FileSpec {
	if ts.GetSymlinkPath() != "" {
		// Symlink case
		// TODO: Do we need to check Size, Hash, and IsExecutable are empty?
		// Currently they're just ignored.
		return &cmdpb.FileSpec{
			Path:    ts.GetPath(),
			Symlink: ts.GetSymlinkPath(),
		}
	}

	// Non symlink case
	return &cmdpb.FileSpec{
		Path:         ts.GetPath(),
		IsExecutable: ts.GetIsExecutable(),
		Size:         ts.GetSize(),
		Hash:         ts.GetHash(),
	}
}

// getCmdFiles dynamically generates cmdFiles from ExecReq for arbitrary toolchain support.
func getCmdFiles(ctx context.Context, req *gomapb.ExecReq) []*cmdpb.FileSpec {
	logger := log.FromContext(ctx)

	if len(req.ToolchainSpecs) == 0 {
		// For backward compatibility, if len(req.ToolchainSpecs) == 0, add a compiler from
		// CommandSpec. After a client that can utilize ToolchainSpecs is rolled, this code
		// can be removed.
		logger.Debugf("toolchain input: command spec: %v", req.CommandSpec)
		return []*cmdpb.FileSpec{
			{
				Path:         req.CommandSpec.GetLocalCompilerPath(),
				IsExecutable: true,
				Size:         req.CommandSpec.GetSize(),
				Hash:         string(req.CommandSpec.GetBinaryHash()),
			},
		}
	}

	var cmdFiles []*cmdpb.FileSpec
	for _, ts := range req.ToolchainSpecs {
		logger.Debugf("toolchain input: toolchain spec: %v", ts)
		cmdFiles = append(cmdFiles, toolchainSpecToFileSpec(ts))
	}

	return cmdFiles
}

func (in *Inventory) pickFromExecReq(ctx context.Context, req *gomapb.ExecReq, resp *gomapb.ExecResp) (*cmdpb.Config, []*cmdpb.FileSpec, error) {
	logger := log.FromContext(ctx)

	dimensions := req.GetRequesterInfo().GetDimensions()
	if len(dimensions) == 0 {
		resp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		resp.ErrorMessage = append(resp.ErrorMessage, "No dimensions are specified")
		return nil, nil, errors.New("no dimensions are specified")
	}

	// Select the best possible platform. If there is multiple possible platforms,
	// select the first one.
	var matchedConfig *platformConfig
	for _, pCfg := range in.platformConfigs {
		if matchDimensions(dimensions, pCfg.dimensionSet) {
			matchedConfig = pCfg
			break
		}
	}

	if matchedConfig == nil {
		resp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		resp.ErrorMessage = append(resp.ErrorMessage, fmt.Sprintf("Could not matching runtime config with dimensions=%v", dimensions))
		return nil, nil, fmt.Errorf("possible platform not found in inventory: dimensions=%v", dimensions)
	}

	cmdSel, _, err := fromCommandSpec(req.GetCommandSpec())
	if err != nil {
		resp.Error = gomapb.ExecResp_BAD_REQUEST.Enum()
		resp.ErrorMessage = append(resp.ErrorMessage, fmt.Sprintf("unexpected CommandSpec %s", req.GetCommandSpec()))

		return nil, nil, fmt.Errorf("normalize %v: %v", req.GetCommandSpec(), err)
	}

	// Dynamically generate cmdpb.Config here.
	cfg := &cmdpb.Config{
		RemoteexecPlatform: matchedConfig.remoteexecPlatform,
		CmdDescriptor: &cmdpb.CmdDescriptor{
			Selector: cmdSel.Proto(),
			Setup: &cmdpb.CmdDescriptor_Setup{
				PathType: pathTypeFromPathStyle(req.GetRequesterInfo().GetPathStyle()),
			},
		},
	}

	cmdFiles := getCmdFiles(ctx, req)

	logger.Infof("pick platform %v", cfg)
	return cfg, cmdFiles, nil
}

// initResult initializes ExecResult from request before command selection.
func initResult(req *gomapb.ExecReq) *gomapb.ExecResult {
	var subprograms []*gomapb.SubprogramSpec
	for _, s := range req.GetSubprogram() {
		subprograms = append(subprograms, &gomapb.SubprogramSpec{
			Path: proto.String(s.GetPath()),
		})
	}

	return &gomapb.ExecResult{
		ExitStatus: proto.Int32(-1),
		CommandSpec: &gomapb.CommandSpec{
			Name:    proto.String(req.GetCommandSpec().GetName()),
			Version: proto.String(req.GetCommandSpec().GetVersion()),
			Target:  proto.String(req.GetCommandSpec().GetTarget()),
		},
		Subprogram: subprograms,
	}
}

// setPicked sets picked command in the ExecResult.
func setPicked(result *gomapb.ExecResult, cfg *cmdpb.Config, path2sel map[string]selector) {
	result.CommandSpec.BinaryHash = []byte(cfg.CmdDescriptor.Selector.BinaryHash)
	// TODO: detailed_info, or so?
	for i, s := range result.Subprogram {
		if ss, found := path2sel[s.GetPath()]; found {
			result.Subprogram[i].BinaryHash = proto.String(ss.BinaryHash)
		}
	}
}

func fromCommandSpec(spec *gomapb.CommandSpec) (selector, string, error) {
	s, err := normalizer.Selector(cmdpb.Selector{
		Name:       spec.GetName(),
		Version:    spec.GetVersion(),
		Target:     spec.GetTarget(),
		BinaryHash: string(spec.GetBinaryHash()),
	})
	if err != nil {
		return selector{}, "", err
	}
	return fromSelectorProto(&s), spec.GetLocalCompilerPath(), nil
}

func fromSubprogramSpec(spec *gomapb.SubprogramSpec) selector {
	return selector{
		Name:       filepath.Base(spec.GetPath()), // TODO: fix
		BinaryHash: string(spec.GetBinaryHash()),
	}
}

func (in *Inventory) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	versionID, resp := in.status()
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "version-id: %s\n", versionID)
	fmt.Fprintln(w)
	for _, cfg := range resp {
		fmt.Fprintf(w, "%v\n", cfg)
	}
}
