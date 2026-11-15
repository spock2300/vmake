package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
	Node                        *BuildNode
	SourceFiles                 []string
	AllIncludes                 []string
	AllDefines                  []string
	AllCFlags                   []string
	AllCxxFlags                 []string
	AllLdFlags                  []string
	AllLinks                    []string
	DepArtifacts                []string
	OutputPath                  string
	VersionScript               string
	LinkerScript                string
	ExcludeLibs                 []string
	SymbolBinding               string
	AllowedPublicIncludes       []allowedInclude
	DirectAllowedPublicIncludes []allowedInclude
	DeclaredDepPkgs             map[string]bool
	DirectDepPkgs               map[string]bool
	Placement                   ArtifactPlacement
}

type compileResult struct {
	src     string
	objPath string
	deps    []string
	changed bool
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
	workspaceDirs map[string]string
	pkgGenerated  map[string]bool
	rootDir       string
	includeTests  bool
	numWorkers    int
	keepGoing     bool
	explain       bool
	hashes        *fileHashCache
	stats         *BuildStats
	session       *Session
	canonCache    *pathCanonCache

	auditor   *headerAuditor
	auditorMu sync.Mutex

	buildTargetFn  func(string) error
	globalCFlags   []string
	globalCxxFlags []string
	globalLdFlags  []string
	globalLinks    []string
}

type schedulerConfig struct {
	Graph         *BuildGraph
	Toolchain     *toolchain.Toolchain
	Platform      api.Platform
	Layouts       map[string]*PackageLayout
	Mode          string
	Options       map[string]map[string]any
	Packages      map[string]*api.Package
	WorkspaceDirs map[string]string
	Tools         *ResolvedTools
	Context       context.Context
	RootDir       string
	IncludeTests  bool
	NumWorkers    int
	KeepGoing     bool
	Explain       bool
	GlobalFlags   *[4][]string
	Stats         *BuildStats
	Session       *Session
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
		workspaceDirs: cfg.WorkspaceDirs,
		pkgGenerated:  make(map[string]bool),
		rootDir:       cfg.RootDir,
		includeTests:  cfg.IncludeTests,
		numWorkers:    cfg.NumWorkers,
		keepGoing:     cfg.KeepGoing,
		explain:       cfg.Explain,
		hashes:        newFileHashCache(),
		stats:         cfg.Stats,
		session:       cfg.Session,
		canonCache:    newPathCanonCache(),
	}
	if s.stats == nil {
		s.stats = NewBuildStats()
	}
	s.auditorFor()
	if cfg.GlobalFlags != nil {
		s.globalCFlags = append([]string{}, cfg.GlobalFlags[0]...)
		s.globalCxxFlags = append([]string{}, cfg.GlobalFlags[1]...)
		s.globalLdFlags = append([]string{}, cfg.GlobalFlags[2]...)
		s.globalLinks = append([]string{}, cfg.GlobalFlags[3]...)
	}

	for pkgName, layout := range cfg.Layouts {
		if layout == nil {
			continue
		}
		s.pkgs[pkgName] = NewPkgInfo(layout.Dirs, layout.BuildKey)
	}

	return s
}

func (s *Scheduler) targetOutcomeFor(node *BuildNode, pkgInfo *PkgInfo, resolved *ResolvedTarget, actionRan bool) *targetOutcome {
	target := node.Target
	status := TargetStatusUpToDate
	if actionRan {
		status = TargetStatusBuilt
	}
	outcome := &targetOutcome{
		FullName: node.FullName,
		Result: TargetResult{
			Package: node.PkgName,
			Target:  target.Name(),
			Kind:    string(target.Kind()),
			Status:  status,
		},
	}
	placement := resolved.Placement
	if placement.Kind == "" {
		placement = s.placementFor(resolved, pkgInfo)
	}
	outcome.Placement = placement
	outcome.Result.Outputs = append([]string{}, placement.PostLinkOutputs...)
	if target.Kind() != api.TargetVoid && target.Kind() != api.TargetObject {
		outcome.Result.Outputs = append([]string{resolveWorkPath(pkgInfo.SourceDir, placement.OutputPath)}, outcome.Result.Outputs...)
	}
	return outcome
}

func (s *Scheduler) auditorFor() *headerAuditor {
	s.auditorMu.Lock()
	defer s.auditorMu.Unlock()
	env := auditEnv{
		graph:         s.graph,
		pkgs:          s.pkgs,
		packages:      s.packages,
		workspaceDirs: s.workspaceDirs,
		rootDir:       s.rootDir,
		canonCache:    s.canonCache,
	}
	if s.auditor == nil || s.auditor.graph != env.graph || s.auditor.rootDir != env.rootDir {
		s.auditor = newHeaderAuditor(env)
	}
	return s.auditor
}

func (s *Scheduler) Stats() *BuildStats {
	return s.stats
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
	start := time.Now()
	outcome, err := s.buildTargetOutcome(fullName)
	if err != nil || outcome == nil {
		return err
	}
	outcome.Result.DurationMs = time.Since(start).Milliseconds()
	if s.stats != nil {
		outcome.Result.Size = s.stats.TargetSize(fullName)
	}
	if s.session != nil {
		s.session.recordTarget(*outcome)
	}
	return nil
}

func (s *Scheduler) buildTargetOutcome(fullName string) (*targetOutcome, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	node, err := s.graph.GetNode(fullName)
	if err != nil {
		return nil, err
	}

	if !node.Target.IsDefault() {
		return nil, nil
	}
	if node.Target.IsTest() && !s.includeTests {
		return nil, nil
	}

	pkgInfo, err := s.pkgInfoOf(node.PkgName)
	if err != nil {
		return nil, wrapTargetError(fullName, "prepare", err)
	}
	workDir := pkgInfo.SourceDir

	vlog.Verbose("[%s]", fullName)

	if err := s.ensurePackageGenerated(node.PkgName); err != nil {
		return nil, wrapTargetError(fullName, "prepare", err)
	}

	resolved, err := s.resolveTarget(node)
	if err != nil {
		return nil, wrapTargetError(fullName, "prepare", err)
	}

	if err := s.prepareTarget(resolved, pkgInfo, workDir); err != nil {
		return nil, wrapTargetError(fullName, "prepare", err)
	}

	stage := "link"
	if node.Target.Kind() == api.TargetVoid {
		stage = "build"
	}
	actionRan := false
	numFiles := len(resolved.SourceFiles)
	if numFiles == 0 || node.Target.Prebuilt() != "" {
		actionRan, err = s.finalizeTarget(resolved, pkgInfo, nil)
	} else {
		var objs []string
		var compiled bool
		objs, compiled, err = s.compileAll(resolved, pkgInfo)
		if err != nil {
			return nil, wrapTargetError(fullName, "compile", err)
		}
		var linked bool
		linked, err = s.finalizeTarget(resolved, pkgInfo, objs)
		actionRan = compiled || linked
	}
	if err != nil {
		return nil, wrapTargetError(fullName, stage, err)
	}
	return s.targetOutcomeFor(node, pkgInfo, resolved, actionRan), nil
}

func (s *Scheduler) ensurePackageGenerated(pkgName string) error {
	if s.pkgGenerated[pkgName] {
		return nil
	}
	if s.pkgGenerated == nil {
		s.pkgGenerated = make(map[string]bool)
	}
	s.pkgGenerated[pkgName] = true
	pkg := s.packages[pkgName]
	if pkg == nil || !api.BindPackageHasGenerators(pkg) {
		return nil
	}
	pkgInfo := s.pkgs[pkgName]
	if pkgInfo == nil {
		return nil
	}
	buildDir := resolveWorkPath(pkgInfo.SourceDir, pkgInfo.EffectiveBuildDir())
	return api.BindPackageRunGenerators(pkg, pkgInfo.SourceDir, buildDir, func(outPath string) {
		if s.hashes != nil {
			s.hashes.evict(pkgInfo.SourceDir, []string{outPath})
		}
	})
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
