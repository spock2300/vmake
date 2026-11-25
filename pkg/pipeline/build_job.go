package pipeline

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

// PackageSource is a package's source location as the build sees it: Root is
// the package directory and Tree is where the sources materialize (a link
// target, a managed checkout, or Root when the package builds in place).
type PackageSource struct {
	Root   string
	Tree   string
	Linked bool
}

// packageSources is the single derivation of per-package source locations; the
// directory maps other stages consume are projections of it.
func packageSources(ctx *RuntimeContext) map[string]PackageSource {
	if ctx == nil || ctx.DepGraph == nil {
		return nil
	}
	sources := make(map[string]PackageSource, len(ctx.DepGraph.Packages))
	for name, node := range ctx.DepGraph.Packages {
		if node == nil || node.Source == nil {
			continue
		}
		source := PackageSource{Root: node.Source.Dir, Tree: node.Source.Dir}
		if node.Source.Link != nil && node.Source.Link.Path != "" {
			source.Linked = true
			source.Tree = node.Source.Link.Path
		}
		if expected := ExpectedPackageSourceDir(ctx, name, node); expected != "" {
			source.Tree = expected
		}
		sources[name] = source
	}
	return sources
}

// packageSourceDirs projects the trees onto the package-root directories the
// configuration and inspection stages consume.
func packageSourceDirs(ctx *RuntimeContext) map[string]*api.PkgDirs {
	dirs := make(map[string]*api.PkgDirs)
	for name, source := range packageSources(ctx) {
		dirs[name] = &api.PkgDirs{SourceDir: source.Root}
	}
	return dirs
}

func workspacePackageDirs(ctx *RuntimeContext) map[string]string {
	sources := packageSources(ctx)
	if sources == nil {
		return nil
	}
	dirs := make(map[string]string, len(sources))
	for name, source := range sources {
		if source.Tree != "" {
			dirs[name] = source.Tree
		}
	}
	return dirs
}

func buildEnvironment(rc *runContext, cfg *configStage, src *sourceStage, decl *declareStage) *build.BuildEnv {
	packages := make(map[string]*api.Package)
	for _, name := range rc.ctx.Resolver.GetOrder() {
		node := rc.ctx.DepGraph.Packages[name]
		if node != nil && node.Pkg != nil {
			packages[name] = node.Pkg
		}
	}
	return &build.BuildEnv{
		Layouts:       src.layouts,
		Packages:      packages,
		WorkspaceDirs: workspacePackageDirs(rc.ctx),
		Toolchains:    decl.packageToolchains(),
		Options:       cfg.allPkgOptions,
		GlobalFlags:   rc.globalFlags,
		Session:       rc.session,
		RootDir:       rc.ctx.Paths.ProjectDir,
		Jobs:          rc.opts.Jobs,
		KeepGoing:     rc.opts.KeepGoing,
	}
}
