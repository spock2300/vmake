package pipeline

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

// packageSourceDirs projects the trees onto the package-root directories the
// configuration and inspection stages consume.
func packageSourceDirs(ctx *RuntimeContext) map[string]*api.PkgDirs {
	dirs := make(map[string]*api.PkgDirs)
	for name, source := range NewSourceWorkspace(ctx).sources() {
		dirs[name] = &api.PkgDirs{SourceDir: source.BuildRoot()}
	}
	return dirs
}

func workspacePackageDirs(ctx *RuntimeContext) map[string]string {
	sources := NewSourceWorkspace(ctx).sources()
	if sources == nil {
		return nil
	}
	dirs := make(map[string]string, len(sources))
	for name, source := range sources {
		if tree := source.SourceTree(); tree != "" {
			dirs[name] = tree
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
