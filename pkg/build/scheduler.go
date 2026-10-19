package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	BuildKey string
}

func (pi *PkgInfo) EffectiveBuildDir() string {
	if pi.BuildDir != "" {
		return pi.BuildDir
	}
	return BuildPath(".", pi.BuildKey, "")
}

func (pi *PkgInfo) ResolvePath(path string) string {
	if pi == nil {
		return path
	}
	return resolvePkgPath(pi.SourceDir, pi.EffectiveBuildDir(), path)
}

func (pi *PkgInfo) OutputPath(subpath string) string {
	if pi.BuildDir != "" {
		return filepath.Join(pi.BuildDir, subpath)
	}
	return BuildPath(".", pi.BuildKey, subpath)
}

func (pi *PkgInfo) GeneratedDir() string {
	if pi.BuildDir != "" {
		return filepath.Join(pi.BuildDir, subdirGenerated)
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
	pkgGenerated  map[string]bool
	rootDir       string
	includeTests  bool
	check         bool
	numWorkers    int
	keepGoing     bool
	explain       bool
	hashes        *fileHashCache
	stats         *BuildStats

	buildTargetFn  func(string) error
	globalCFlags   []string
	globalCxxFlags []string
	globalLdFlags  []string
	globalLinks    []string
}

type schedulerConfig struct {
	Graph        *BuildGraph
	Toolchain    *toolchain.Toolchain
	Platform     api.Platform
	Layouts      map[string]*PackageLayout
	Mode         string
	Options      map[string]map[string]any
	Packages     map[string]*api.Package
	Tools        *ResolvedTools
	Context      context.Context
	RootDir      string
	IncludeTests bool
	Check        bool
	NumWorkers   int
	KeepGoing    bool
	Explain      bool
	GlobalFlags  *[4][]string
	Stats        *BuildStats
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
	if mode == "" {
		mode = api.ModeDebug
	}
	layouts := make(map[string]*PackageLayout, len(pkgDirs))
	for name, dirs := range pkgDirs {
		key, err := BuildKeyE(tools.CCKey(), mode, pkgOptions[name], "")
		if err != nil {
			return nil, err
		}
		layouts[name] = &PackageLayout{Dirs: *dirs, BuildKey: key, Ready: true}
	}
	return newScheduler(schedulerConfig{
		Graph:     graph,
		Toolchain: tc,
		Platform:  platform,
		Layouts:   layouts,
		Mode:      mode,
		Options:   pkgOptions,
		Tools:     tools,
	}), nil
}

func newScheduler(cfg schedulerConfig) *Scheduler {
	if cfg.Mode == "" {
		cfg.Mode = api.ModeDebug
	}
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	compiler := NewCompiler(cfg.Tools)
	linker := NewLinker(cfg.Tools)
	linker.run = gnuRunnerContext(cfg.Context, cfg.Tools.env)
	manager := toolchain.GetManager()
	packages := cfg.Packages
	if packages == nil {
		packages = make(map[string]*api.Package)
	}

	s := &Scheduler{
		ctx:           cfg.Context,
		graph:         cfg.Graph,
		compiler:      compiler,
		linker:        linker,
		toolchain:     cfg.Toolchain,
		platform:      cfg.Platform,
		resolvedTools: cfg.Tools,
		mode:          cfg.Mode,
		pkgOptions:    cfg.Options,
		pkgs:          make(map[string]*PkgInfo),
		ccWriter:      NewCompileCommandsWriter(cfg.Tools),
		packages:      packages,
		pkgGenerated:  make(map[string]bool),
		rootDir:       cfg.RootDir,
		includeTests:  cfg.IncludeTests,
		check:         cfg.Check,
		numWorkers:    cfg.NumWorkers,
		keepGoing:     cfg.KeepGoing,
		explain:       cfg.Explain,
		hashes:        newFileHashCache(),
		stats:         cfg.Stats,
	}
	if s.stats == nil {
		s.stats = NewBuildStats()
	}
	if cfg.GlobalFlags != nil {
		s.globalCFlags = append([]string{}, cfg.GlobalFlags[0]...)
		s.globalCxxFlags = append([]string{}, cfg.GlobalFlags[1]...)
		s.globalLdFlags = append([]string{}, cfg.GlobalFlags[2]...)
		s.globalLinks = append([]string{}, cfg.GlobalFlags[3]...)
	} else {
		s.globalCFlags = append([]string{}, manager.GetGlobalCFlags()...)
		s.globalCxxFlags = append([]string{}, manager.GetGlobalCxxFlags()...)
		s.globalLdFlags = append([]string{}, manager.GetGlobalLdFlags()...)
		s.globalLinks = append([]string{}, manager.GetGlobalLinks()...)
	}

	for pkgName, layout := range cfg.Layouts {
		if layout == nil {
			continue
		}
		s.pkgs[pkgName] = &PkgInfo{
			PkgDirs:  layout.Dirs,
			BuildKey: layout.BuildKey,
		}
	}

	return s
}

func (s *Scheduler) Stats() *BuildStats {
	return s.stats
}

func (s *Scheduler) TargetDuration(fullName string) int64 {
	if s.stats == nil {
		return 0
	}
	return s.stats.TargetDuration(fullName)
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
		return nil, fmt.Errorf("package %s has no build dirs registered", pkgName)
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
	if !s.check {
		if err := s.ccWriter.Save(filepath.Join(root, "build", "compile_commands.json")); err != nil {
			errs = append(errs, err)
		}
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
	start := time.Now()
	defer func() {
		if s.stats != nil {
			s.stats.RecordTargetDuration(fullName, time.Since(start))
		}
	}()
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
		return wrapTargetError(fullName, "prepare", err)
	}
	workDir := pkgInfo.SourceDir

	vlog.Verbose("[%s]", fullName)

	if err := s.ensurePackageGenerated(node.PkgName); err != nil {
		return wrapTargetError(fullName, "prepare", err)
	}

	resolved, err := s.resolveTarget(node)
	if err != nil {
		return wrapTargetError(fullName, "prepare", err)
	}

	if err := s.prepareTarget(resolved, pkgInfo, workDir); err != nil {
		return wrapTargetError(fullName, "prepare", err)
	}

	stage := "link"
	if node.Target.Kind() == api.TargetVoid {
		stage = "build"
	}
	numFiles := len(resolved.SourceFiles)
	if numFiles == 0 || node.Target.Prebuilt() != "" {
		return wrapTargetError(fullName, stage, s.finalizeTarget(resolved, pkgInfo, nil))
	}

	objs, err := s.compileAll(resolved, pkgInfo)
	if err != nil {
		return wrapTargetError(fullName, "compile", err)
	}

	return wrapTargetError(fullName, stage, s.finalizeTarget(resolved, pkgInfo, objs))
}

func (s *Scheduler) ensurePackageGenerated(pkgName string) error {
	if s.check || s.pkgGenerated[pkgName] {
		return nil
	}
	if s.pkgGenerated == nil {
		s.pkgGenerated = make(map[string]bool)
	}
	s.pkgGenerated[pkgName] = true
	pkg := s.packages[pkgName]
	if pkg == nil || !pkg.HasGenerators() {
		return nil
	}
	pkgInfo := s.pkgs[pkgName]
	if pkgInfo == nil {
		return nil
	}
	buildDir := resolveWorkPath(pkgInfo.SourceDir, pkgInfo.EffectiveBuildDir())
	return pkg.RunGenerators(pkgInfo.SourceDir, buildDir, func(outPath string) {
		if s.hashes != nil {
			s.hashes.evict(pkgInfo.SourceDir, []string{outPath})
		}
	})
}

func (s *Scheduler) prepareTarget(resolved *ResolvedTarget, pkgInfo *PkgInfo, workDir string) error {
	if s.check {
		node := resolved.Node
		pkg := s.packages[node.PkgName]
		if len(node.Target.GenRules()) > 0 || (pkg != nil && (pkg.GenConfigHeader() || pkg.HasGenerators())) {
			if s.stats != nil {
				s.stats.RecordPlannedGenerator(node.FullName)
			}
		}
		return nil
	}
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
