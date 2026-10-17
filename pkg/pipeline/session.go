package pipeline

import (
	"encoding/json"
	"fmt"
	"maps"
	"runtime"

	"github.com/spock2300/vmake/internal/buildruntime"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type packageBinding struct {
	signature string
	values    map[string]any
	toolchain *toolchain.Toolchain
}

func (s *declareStage) packageBindingValues(name string, base map[string]any) (map[string]any, error) {
	if base == nil {
		base = s.rc.cfg.GlobalValues
	}
	ctx := *s.rc.ctx
	cfg := *s.rc.ctx.Config
	cfg.Entries = maps.Clone(cfg.Entries)
	if entry := cfg.Entries[name]; entry != nil {
		copy := *entry
		cfg.Entries[name] = &copy
	}
	ctx.Config = &cfg
	values, err := PackageConfigValues(&ctx, name, base)
	if err != nil {
		return nil, err
	}
	baseTC, _ := base[api.ToolchainOptionName].(string)
	if baseTC == "" {
		baseTC = s.rc.cfg.TcName
	}
	tcName := resolvePkgToolchain(&cfg, name, baseTC)
	values[api.ToolchainOptionName] = tcName
	return values, nil
}

func (s *declareStage) subGraphRequestSignature(name string, values, scope map[string]any) (string, error) {
	encoded, err := json.Marshal(struct {
		Values map[string]any
		Scope  map[string]any
		Flags  [4][]string
	}{values, scope, s.rc.globalFlags})
	if err != nil {
		return "", fmt.Errorf("subgraph package %s configuration: %w", name, err)
	}
	return string(encoded), nil
}

func (s *declareStage) packageBindingSignature(name string, values map[string]any, tc *toolchain.Toolchain) (string, error) {
	encoded, err := json.Marshal(struct {
		Values    map[string]any
		Toolchain *toolchain.Toolchain
		Flags     [4][]string
	}{values, tc, s.rc.globalFlags})
	if err != nil {
		return "", fmt.Errorf("package %s configuration: %w", name, err)
	}
	return string(encoded), nil
}

func (s *declareStage) matchingPackageBinding(name string, values map[string]any) (*packageBinding, error) {
	bound := s.bindings[name]
	if bound == nil {
		return nil, nil
	}
	tcName, _ := values[api.ToolchainOptionName].(string)
	incompatible := func() error {
		return fmt.Errorf("package %s was already bound to an incompatible build configuration (toolchain %s requested as %s)", name, bound.toolchain.Name, tcName)
	}
	if bound.values[api.ToolchainOptionName] != tcName {
		return nil, incompatible()
	}
	tc := s.rc.cfg.Tc
	if tcName != s.rc.cfg.TcName {
		var err error
		tc, err = toolchain.GetManager().GetToolchain(tcName)
		if err != nil {
			return nil, err
		}
	}
	signature, err := s.packageBindingSignature(name, values, tc)
	if err != nil {
		return nil, err
	}
	if bound.signature != signature {
		return nil, incompatible()
	}
	return bound, nil
}

func (s *declareStage) bindPackage(name string) (*packageBinding, error) {
	values, err := s.packageBindingValues(name, s.scopeValues)
	if err != nil {
		return nil, err
	}
	if bound, err := s.matchingPackageBinding(name, values); bound != nil || err != nil {
		return bound, err
	}
	tcName, _ := values[api.ToolchainOptionName].(string)
	tc := s.rc.cfg.Tc
	if tcName != s.rc.cfg.TcName {
		tc, err = toolchain.GetManager().SelectToolchain(tcName)
		if err != nil {
			return nil, err
		}
	}
	signature, err := s.packageBindingSignature(name, values, tc)
	if err != nil {
		return nil, err
	}
	copyTC := *tc
	bound := &packageBinding{signature: signature, values: maps.Clone(values), toolchain: &copyTC}
	s.bindings[name] = bound
	platform := platformFromValues(values)
	tools, err := s.rc.session.ResolveTools(&copyTC, platform)
	if err != nil {
		return nil, err
	}
	node := s.rc.ctx.DepGraph.Packages[name]
	scriptHash, err := s.source.scriptHashFor(name)
	if err != nil {
		return nil, err
	}
	flagsHash := packageFlagsHash(s.rc.globalFlagsHash, node)
	dirs := s.source.pkgDirs[name]
	if dirs == nil {
		return nil, fmt.Errorf("package %s has no build directories", name)
	}
	if node.IsLocal() {
		s.source.pkgDirs[name] = makeLocalPkgDirs(dirs.SourceDir, newLocalPkgKey(tools.CCKey(), s.rc.cfg.Mode, s.conf.allPkgOptions[name], flagsHash, scriptHash, s.source.sourceCommits[name]))
	} else {
		version, commit := s.source.remotePkgKeyMaterial(name)
		owner := remoteOwnerName(s.rc.ctx, name)
		member := remoteMemberPath(s.rc.ctx, name)
		tree := s.source.remote.trees[name]
		if tree == "" {
			tree = s.source.remote.trees[owner]
		}
		if tree == "" {
			tree = remoteTreeDir(s.rc.ctx, owner)
		}
		s.source.pkgDirs[name] = makeRemotePkgDirs(tree, member, newRemotePkgKey(tools.CCKey(), s.rc.cfg.Mode, s.conf.allPkgOptions[name], version, s.source.packageCommitKey(name, commit), flagsHash, s.source.patchHashes[name], scriptHash))
	}
	if err := s.source.preparePackageWorkspace(name); err != nil {
		return nil, err
	}
	currentDirs := s.source.pkgDirs[name]
	if dirs.BuildDir != currentDirs.BuildDir || dirs.SourceDir != currentDirs.SourceDir {
		if err := s.source.applyPatchesFor(name); err != nil {
			return nil, err
		}
		if err := rebaseKConfigSourceDirs(s.rc.ctx, name, dirs.SourceDir, currentDirs.SourceDir); err != nil {
			return nil, err
		}
		if err := restoreKConfigFiles(s.rc.ctx, s.source.pkgDirs, map[string]bool{name: true}); err != nil {
			return nil, err
		}
	}
	if s.rc.budget == nil {
		s.rc.budget = &buildruntime.Budget{Jobs: runtime.NumCPU()}
	}
	if err := api.BindBuildRuntime(node.Pkg, s.rc.budget, s.rc.ctx.Context); err != nil {
		return nil, err
	}
	node.Pkg.SetGlobalFlags(s.rc.globalFlags[0], s.rc.globalFlags[1], s.rc.globalFlags[2], s.rc.globalFlags[3])
	return bound, nil
}

func (s *declareStage) packageToolchains() map[string]*toolchain.Toolchain {
	result := make(map[string]*toolchain.Toolchain, len(s.bindings))
	for name, bound := range s.bindings {
		result[name] = bound.toolchain
	}
	return result
}
