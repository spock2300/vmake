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

func (s *buildPhaseState) acquireStorageOwners() error {
	if s.ctx.Locks == nil {
		return nil
	}
	parents := s.ctx.Resolver.SubParents()
	var owners []string
	for name := range s.needed {
		node := s.ctx.DepGraph.Packages[name]
		if node == nil || node.IsLocal() {
			continue
		}
		for parents[name] != "" {
			name = parents[name]
		}
		owners = append(owners, name)
	}
	return s.ctx.Locks.AcquireOwnersContext(s.ctx.Context, owners)
}

func (s *buildPhaseState) packageBindingValues(name string, base map[string]any) (map[string]any, error) {
	if base == nil {
		base = s.cfg.GlobalValues
	}
	ctx := *s.ctx
	cfg := *s.ctx.Config
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
		baseTC = s.cfg.TcName
	}
	tcName := resolvePkgToolchain(&cfg, name, baseTC)
	values[api.ToolchainOptionName] = tcName
	return values, nil
}

func (s *buildPhaseState) subGraphRequestSignature(name string, values, scope map[string]any) (string, error) {
	encoded, err := json.Marshal(struct {
		Values map[string]any
		Scope  map[string]any
		Flags  [4][]string
	}{values, scope, s.globalFlags})
	if err != nil {
		return "", fmt.Errorf("subgraph package %s configuration: %w", name, err)
	}
	return string(encoded), nil
}

func (s *buildPhaseState) packageBindingSignature(name string, values map[string]any, tc *toolchain.Toolchain) (string, error) {
	encoded, err := json.Marshal(struct {
		Values    map[string]any
		Toolchain *toolchain.Toolchain
		Flags     [4][]string
	}{values, tc, s.globalFlags})
	if err != nil {
		return "", fmt.Errorf("package %s configuration: %w", name, err)
	}
	return string(encoded), nil
}

func (s *buildPhaseState) matchingPackageBinding(name string, values map[string]any) (*packageBinding, error) {
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
	tc := s.cfg.Tc
	if tcName != s.cfg.TcName {
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

func (s *buildPhaseState) bindPackage(name string) (*packageBinding, error) {
	values, err := s.packageBindingValues(name, s.scopeValues)
	if err != nil {
		return nil, err
	}
	if bound, err := s.matchingPackageBinding(name, values); bound != nil || err != nil {
		return bound, err
	}
	tcName, _ := values[api.ToolchainOptionName].(string)
	tc := s.cfg.Tc
	if tcName != s.cfg.TcName {
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
	tools, err := s.session.ResolveTools(&copyTC, platform)
	if err != nil {
		return nil, err
	}
	node := s.ctx.DepGraph.Packages[name]
	scriptHash, err := s.scriptHashFor(name)
	if err != nil {
		return nil, err
	}
	flagsHash := packageFlagsHash(s.globalFlagsHash, node)
	dirs := s.pkgDirs[name]
	if dirs == nil {
		return nil, fmt.Errorf("package %s has no build directories", name)
	}
	if node.IsLocal() {
		s.pkgDirs[name] = makeLocalPkgDirs(dirs.SourceDir, tools.CCKey(), s.cfg.Mode, s.allPkgOptions[name], flagsHash, scriptHash, s.sourceCommits[name])
	} else {
		version, commit := s.remotePkgKeyMaterial(name)
		if versionDir := s.remote.versionDirs[name]; versionDir != "" {
			s.pkgDirs[name] = makeRemotePkgDirs(versionDir, dirs.SourceDir, tools.CCKey(), s.cfg.Mode, s.allPkgOptions[name], version, s.packageCommitKey(name, commit), flagsHash, s.patchHashes[name], scriptHash, remoteMemberPath(s.ctx, name))
		}
	}
	if err := s.preparePackageWorkspace(name); err != nil {
		return nil, err
	}
	currentDirs := s.pkgDirs[name]
	if dirs.BuildDir != currentDirs.BuildDir || dirs.SourceDir != currentDirs.SourceDir {
		if err := applyPatchesContext(s.ctx.Context, node.Pkg, node.Pkg.SrcDir()); err != nil {
			return nil, fmt.Errorf("apply patches for %s: %w", name, err)
		}
		if err := rebaseKConfigSourceDirs(s.ctx, name, dirs.SourceDir, currentDirs.SourceDir); err != nil {
			return nil, err
		}
		if err := restoreKConfigFiles(s.ctx, s.pkgDirs, map[string]bool{name: true}); err != nil {
			return nil, err
		}
	}
	if s.budget == nil {
		s.budget = &buildruntime.Budget{Jobs: runtime.NumCPU()}
	}
	if err := api.BindBuildRuntime(node.Pkg, s.budget, s.ctx.Context); err != nil {
		return nil, err
	}
	node.Pkg.SetGlobalFlags(s.globalFlags[0], s.globalFlags[1], s.globalFlags[2], s.globalFlags[3])
	return bound, nil
}

func (s *buildPhaseState) packageToolchains() map[string]*toolchain.Toolchain {
	result := make(map[string]*toolchain.Toolchain, len(s.bindings))
	for name, bound := range s.bindings {
		result[name] = bound.toolchain
	}
	return result
}
