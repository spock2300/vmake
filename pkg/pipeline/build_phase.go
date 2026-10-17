package pipeline

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spock2300/vmake/internal/buildruntime"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type BuildResult struct {
	Targets       []build.TargetResult
	InstallFiles  []build.InstalledFile
	SubGraphRoots map[string]bool
	AllTargets    map[string]map[string]*api.Target
	Graph         *build.BuildGraph
	PkgDirs       map[string]*api.PkgDirs
	PkgBuildKeys  map[string]string
	PkgPlatforms  map[string]api.Platform
	GlobalValues  map[string]any
	TcName        string
	TargetOS      string
	Mode          string
	InstalledPkgs map[string]*api.InstalledPackage
	BuildCtxs     map[string]*api.BuildContext
}

type remoteVersionState struct {
	entries map[string]*config.EntryConfig
	commits map[string]string
	trees   map[string]string
}

type BuildOptions struct {
	IncludeTests bool
	Jobs         int
	KeepGoing    bool
}

func newRunContext(ctx *RuntimeContext, opts BuildOptions, cfg *buildConfig) *runContext {
	var budget *buildruntime.Budget
	if opts.Jobs > 0 {
		budget = &buildruntime.Budget{Jobs: opts.Jobs}
	}
	return &runContext{
		ctx:     ctx,
		opts:    opts,
		cfg:     cfg,
		session: build.NewSession(ctx.Context),
		budget:  budget,
	}
}

func RunBuild(ctx *RuntimeContext, opts BuildOptions) (_ *BuildResult, err error) {
	defer scriptcall.Recover(&err)
	if ctx.Context != nil && ctx.Context.Err() != nil {
		return nil, ctx.Context.Err()
	}
	if opts.Jobs < 0 {
		return nil, fmt.Errorf("jobs must be non-negative")
	}
	if opts.Jobs == 0 {
		opts.Jobs = runtime.NumCPU()
	}

	cfg, err := resolveBuildConfig(ctx)
	if err != nil {
		return nil, err
	}
	rc := newRunContext(ctx, opts, cfg)

	conf, err := prepareConfig(rc)
	if err != nil {
		return nil, err
	}

	src, err := prepareSources(rc, conf)
	if err != nil {
		return nil, err
	}

	decl, err := declareTargets(rc, conf, src)
	if err != nil {
		return nil, err
	}

	plan, err := planBuild(rc, conf, src, decl)
	if err != nil {
		return nil, err
	}

	return executePlan(rc, conf, src, decl, plan)
}

func UpdateLock(ctx *RuntimeContext) (err error) {
	defer scriptcall.Recover(&err)
	cfg, err := resolveExistingBuildConfig(ctx)
	if err != nil {
		return err
	}
	rc := newRunContext(ctx, BuildOptions{}, cfg)

	conf, err := prepareConfig(rc)
	if err != nil {
		return err
	}

	src, err := prepareSourcesForLock(rc, conf)
	if err != nil {
		return err
	}

	return src.writeLockfile()
}

func newSourceStage(rc *runContext, cfg *configStage) *sourceStage {
	return &sourceStage{
		rc:             rc,
		conf:           cfg,
		pkgDirs:        ResolveAllPackageDirs(rc.ctx.DepGraph),
		patchHashes:    make(map[string]string),
		sourceSeeds:    make(map[string]string),
		sourceCommits:  make(map[string]string),
		sourceVersions: make(map[string]string),
		scriptHashes:   make(map[string]string),
	}
}

func prepareConfig(rc *runContext) (*configStage, error) {
	needed, err := filterAndCollectNeeded(rc.ctx)
	if err != nil {
		return nil, err
	}
	if err := acquireStorageOwners(rc.ctx, needed); err != nil {
		return nil, err
	}
	applyGlobalFlagsFromNeeded(rc.ctx, needed)

	mgr := toolchain.GetManager()
	rc.globalFlags = [4][]string{
		mgr.GetGlobalCFlags(),
		mgr.GetGlobalCxxFlags(),
		mgr.GetGlobalLdFlags(),
		mgr.GetGlobalLinks(),
	}
	rc.globalFlagsHash = build.GlobalFlagsHash()

	return &configStage{
		needed:        needed,
		allPkgOptions: collectAllPkgOptions(rc.ctx, needed),
	}, nil
}

func prepareSourcesForLock(rc *runContext, cfg *configStage) (*sourceStage, error) {
	src := newSourceStage(rc, cfg)
	if err := src.prepareAllPackages(); err != nil {
		return nil, err
	}
	return src, nil
}

func prepareSources(rc *runContext, cfg *configStage) (*sourceStage, error) {
	src, err := prepareSourcesForLock(rc, cfg)
	if err != nil {
		return nil, err
	}
	if err := src.writeLockfile(); err != nil {
		return nil, err
	}
	if err := src.applyPatchesToNeeded(); err != nil {
		return nil, err
	}
	if err := src.restoreKConfigs(); err != nil {
		return nil, err
	}
	if err := os.Remove(filepath.Join(rc.ctx.Paths.ProjectDir, "build", "compile_commands.json")); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return src, nil
}

func declareTargets(rc *runContext, cfg *configStage, src *sourceStage) (*declareStage, error) {
	decl := &declareStage{
		rc:             rc,
		conf:           cfg,
		source:         src,
		bindings:       make(map[string]*packageBinding),
		declarations:   make(map[string]uint8),
		subGraphErrors: make(map[string]error),
		subGraphBuilt:  make(map[string]bool),
	}
	if err := decl.executeOnBuild(); err != nil {
		return nil, err
	}
	decl.logResults()
	return decl, nil
}

func planBuild(rc *runContext, cfg *configStage, src *sourceStage, decl *declareStage) (*planStage, error) {
	graph, err := build.NewBuildGraph(decl.allTargets, decl.pkgMetaMap, rc.ctx.Resolver.SubParents())
	if err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Build order:")
	for _, fullName := range graph.Order {
		vlog.Info("  - %s", fullName)
	}

	keyExtra, err := buildPkgKeyExtra(rc, cfg, src)
	if err != nil {
		return nil, err
	}

	bp := build.NewBuildPipeline(graph, rc.cfg.Tc, src.pkgDirs, rc.cfg.Mode, cfg.allPkgOptions, rc.cfg.Platform)
	bp.SetRootDir(rc.ctx.Paths.ProjectDir)
	bp.SetIncludeTests(rc.opts.IncludeTests)
	bp.SetPkgKeyExtra(keyExtra)
	bp.Session = rc.session
	bp.GlobalFlags = &rc.globalFlags
	bp.PackageToolchains = decl.packageToolchains()
	bp.SetNumWorkers(rc.opts.Jobs)
	bp.SetKeepGoing(rc.opts.KeepGoing)

	for _, name := range rc.ctx.Resolver.GetOrder() {
		node := rc.ctx.DepGraph.Packages[name]
		if cfg.needed[name] && node.Pkg != nil {
			bp.SetPackage(name, node.Pkg)
		}
	}

	vlog.Info("")
	vlog.Info("Building...")

	return &planStage{
		graph:    graph,
		pipeline: bp,
	}, nil
}

func executePlan(rc *runContext, cfg *configStage, src *sourceStage, decl *declareStage, plan *planStage) (*BuildResult, error) {
	scheduler, err := plan.pipeline.Run()
	if err != nil {
		return nil, err
	}

	vlog.Info("")
	vlog.Info("Build succeeded!")

	pkgBuildKeys := make(map[string]string)
	pkgPlatforms := make(map[string]api.Platform)
	for _, name := range rc.ctx.Resolver.GetOrder() {
		node := rc.ctx.DepGraph.Packages[name]
		if node != nil && cfg.needed[name] {
			if node.Pkg != nil {
				pkgPlatforms[name] = api.Platform{OS: node.Pkg.TargetOS(), Triple: node.Pkg.TargetTriple()}
			}
			if info, ok := scheduler.GetPkgInfo(name); ok {
				pkgBuildKeys[name] = info.BuildKey
			}
		}
	}

	return &BuildResult{
		Targets:       rc.session.TargetResults(),
		SubGraphRoots: maps.Clone(decl.subGraphBuilt),
		AllTargets:    decl.allTargets,
		Graph:         plan.graph,
		PkgDirs:       src.pkgDirs,
		PkgBuildKeys:  pkgBuildKeys,
		PkgPlatforms:  pkgPlatforms,
		GlobalValues:  maps.Clone(rc.cfg.GlobalValues),
		TcName:        rc.cfg.TcName,
		TargetOS:      rc.cfg.Platform.OSOrHost(),
		Mode:          rc.cfg.Mode,
		InstalledPkgs: src.remote.installedPkgs(src.pkgDirs),
		BuildCtxs:     decl.buildCtxs,
	}, nil
}
