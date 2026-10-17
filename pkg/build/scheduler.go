package build

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

const (
	subdirObjects   = "object"
	subdirGenerated = "generated"
)

type ResolvedTarget struct {
	Node          *BuildNode
	SourceFiles   []string
	AllIncludes   []string
	AllDefines    []string
	AllCFlags     []string
	AllCxxFlags   []string
	AllLdFlags    []string
	AllLinks      []string
	DepArtifacts  []string
	OutputPath    string
	VersionScript string
	LinkerScript  string
	ExcludeLibs   []string
	SymbolBinding string
}

type compileResult struct {
	src     string
	objPath string
	deps    []string
	err     error
}

type PkgInfo struct {
	api.PkgDirs
	OutputDir string
	BuildKey  string
}

func (pi *PkgInfo) OutputPath(subpath string) string {
	if pi.OutputDir != "" {
		return filepath.Join(pi.OutputDir, subpath)
	}
	return BuildPath(".", pi.BuildKey, subpath)
}

func (pi *PkgInfo) GeneratedDir() string {
	if pi.OutputDir != "" {
		return filepath.Join(pi.OutputDir, subdirGenerated)
	}
	return BuildPath(".", pi.BuildKey, subdirGenerated)
}

type Scheduler struct {
	ctx           context.Context
	graph         *BuildGraph
	compiler      *Compiler
	linker        *Linker
	toolchain     *toolchain.Toolchain
	platform      api.Platform
	resolvedTools *ResolvedTools
	mode          string
	pkgOptions    map[string]map[string]any
	pkgs          map[string]*PkgInfo
	ccWriter      *CompileCommandsWriter
	packages      map[string]*api.Package
	rootDir       string
	includeTests  bool
	pkgKeyExtra   map[string]string
	numWorkers    int
	keepGoing     bool

	buildTargetFn  func(string) error
	globalCFlags   []string
	globalCxxFlags []string
	globalLdFlags  []string
	globalLinks    []string
}

func NewScheduler(
	graph *BuildGraph,
	tc *toolchain.Toolchain,
	pkgDirs map[string]*api.PkgDirs,
	mode string,
	pkgOptions map[string]map[string]any,
	platform api.Platform,
) (*Scheduler, error) {
	tools, err := ResolveTools(tc, platform)
	if err != nil {
		return nil, err
	}
	return newScheduler(graph, tc, pkgDirs, mode, pkgOptions, platform, tools), nil
}

func newScheduler(graph *BuildGraph, tc *toolchain.Toolchain, pkgDirs map[string]*api.PkgDirs, mode string, pkgOptions map[string]map[string]any, platform api.Platform, tools *ResolvedTools) *Scheduler {
	compiler := NewCompiler(tools)
	linker := NewLinker(tools)

	if mode == "" {
		mode = api.ModeDebug
	}

	ccWriter := NewCompileCommandsWriter(tools)
	manager := toolchain.GetManager()

	s := &Scheduler{
		ctx:            context.Background(),
		graph:          graph,
		compiler:       compiler,
		linker:         linker,
		toolchain:      tc,
		platform:       platform,
		resolvedTools:  tools,
		mode:           mode,
		pkgOptions:     pkgOptions,
		pkgs:           make(map[string]*PkgInfo),
		ccWriter:       ccWriter,
		packages:       make(map[string]*api.Package),
		globalCFlags:   append([]string{}, manager.GetGlobalCFlags()...),
		globalCxxFlags: append([]string{}, manager.GetGlobalCxxFlags()...),
		globalLdFlags:  append([]string{}, manager.GetGlobalLdFlags()...),
		globalLinks:    append([]string{}, manager.GetGlobalLinks()...),
	}

	for pkgName, pd := range pkgDirs {
		buildKey := BuildKey(tools.CCKey(), mode, pkgOptions[pkgName], s.pkgExtra(pkgName))
		info := &PkgInfo{
			PkgDirs:  *pd,
			BuildKey: buildKey,
		}
		if pd.BuildDir != "" {
			info.OutputDir = pd.BuildDir
		}
		s.pkgs[pkgName] = info
	}

	return s
}

func (s *Scheduler) SetPackage(pkgName string, pkg *api.Package) {
	s.packages[pkgName] = pkg
}

func (s *Scheduler) SetRootDir(dir string) {
	s.rootDir = dir
}

func (s *Scheduler) SetIncludeTests(v bool) {
	s.includeTests = v
}

func (s *Scheduler) SetPkgKeyExtra(extra map[string]string) {
	s.pkgKeyExtra = maps.Clone(extra)
	for name, info := range s.pkgs {
		info.BuildKey = BuildKey(s.toolIdentity(), s.mode, s.pkgOptions[name], s.pkgExtra(name))
	}
}

func (s *Scheduler) SetNumWorkers(n int) {
	s.numWorkers = n
}

func (s *Scheduler) SetKeepGoing(v bool) {
	s.keepGoing = v
}

func (s *Scheduler) SetBuildTargetFunc(fn func(string) error) {
	s.buildTargetFn = fn
}

func (s *Scheduler) pkgExtra(pkgName string) string {
	if s.pkgKeyExtra == nil {
		return ""
	}
	return s.pkgKeyExtra[pkgName]
}

func (s *Scheduler) SetPkgDirs(pkgName string, dirs *api.PkgDirs) {
	if info, ok := s.pkgs[pkgName]; ok {
		info.PkgDirs = *dirs
	} else {
		s.pkgs[pkgName] = &PkgInfo{PkgDirs: *dirs}
	}
}

func (s *Scheduler) GetPkgInfo(pkgName string) (*PkgInfo, bool) {
	info, ok := s.pkgs[pkgName]
	return info, ok
}

func (s *Scheduler) effectiveSourceDir(pkgName string) string {
	if pkg := s.packages[pkgName]; pkg != nil && pkg.SrcDirRaw() != "" {
		return pkg.SrcDir()
	}
	if info := s.pkgs[pkgName]; info != nil {
		return info.SourceDir
	}
	return ""
}

func (s *Scheduler) pkgInfoOf(pkgName string) (*PkgInfo, error) {
	info := s.pkgs[pkgName]
	if info == nil {
		return nil, fmt.Errorf("package %s has no build dirs registered (SetPkgDirs not called)", pkgName)
	}
	return info, nil
}

func (s *Scheduler) BuildAll() error {
	var errs []error
	failedTargets := make(map[string]bool)
	buildTarget := s.buildTargetFn
	if buildTarget == nil {
		buildTarget = s.Build
	}
	err := s.graph.ForEachDefault(s.includeTests, func(node *BuildNode) error {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if s.depsFailed(node, failedTargets) {
			vlog.Info("[skip] %s (dependency failed)", node.FullName)
			failedTargets[node.FullName] = true
			return nil
		}
		if err := buildTarget(node.FullName); err != nil {
			failedTargets[node.FullName] = true
			errs = append(errs, err)
			if !s.keepGoing || s.ctx.Err() != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && len(errs) == 0 {
		errs = append(errs, err)
	}
	root := s.rootDir
	if root == "" {
		root = "."
	}
	if err := s.ccWriter.Save(filepath.Join(root, "build", "compile_commands.json")); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Scheduler) depsFailed(node *BuildNode, failedTargets map[string]bool) bool {
	for _, dep := range node.Deps {
		if failedTargets[dep] {
			return true
		}
	}
	return false
}

func (s *Scheduler) Build(fullName string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	node, err := s.graph.GetNode(fullName)
	if err != nil {
		return err
	}

	if !node.Target.IsDefault() {
		return nil
	}
	if node.Target.IsTest() && !s.includeTests {
		return nil
	}

	pkgInfo, err := s.pkgInfoOf(node.PkgName)
	if err != nil {
		return err
	}
	workDir := pkgInfo.SourceDir

	vlog.Info("[%s]", fullName)

	resolved, err := s.resolveTarget(node)
	if err != nil {
		return err
	}

	if err := s.prepareTarget(resolved, pkgInfo, workDir); err != nil {
		return err
	}

	numFiles := len(resolved.SourceFiles)
	if numFiles == 0 || node.Target.Prebuilt() != "" {
		return s.finalizeTarget(resolved, pkgInfo, nil)
	}

	objs, err := s.compileAll(resolved, pkgInfo)
	if err != nil {
		return err
	}

	return s.finalizeTarget(resolved, pkgInfo, objs)
}

func (s *Scheduler) prepareTarget(resolved *ResolvedTarget, pkgInfo *PkgInfo, workDir string) error {
	node := resolved.Node

	genRules := node.Target.GenRules()
	if len(genRules) > 0 {
		generatedDir := pkgInfo.GeneratedDir()
		if err := runGenRulesContext(s.ctx, genRules, resolveWorkPath(workDir, generatedDir), workDir); err != nil {
			return err
		}
	}

	pkg := s.packages[node.PkgName]
	if pkg != nil && pkg.GenConfigHeader() {
		generatedDir := pkgInfo.GeneratedDir()
		if err := s.generateConfigHeader(pkg, resolveWorkPath(workDir, generatedDir)); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(resolveWorkPath(workDir, pkgInfo.OutputPath(subdirObjects)), 0755); err != nil {
		return fmt.Errorf("create build directory: %w", err)
	}
	return nil
}

func unique(s []string) []string {
	if len(s) == 0 {
		return s
	}
	seen := make(map[string]bool, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
