package pipeline

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

func workspacePackageDirs(ctx *RuntimeContext) map[string]string {
	if ctx == nil || ctx.DepGraph == nil {
		return nil
	}
	dirs := make(map[string]string, len(ctx.DepGraph.Packages))
	for name, node := range ctx.DepGraph.Packages {
		if node == nil || node.Source == nil {
			continue
		}
		if expected := ExpectedPackageSourceDir(ctx, name, node); expected != "" {
			dirs[name] = expected
		} else if node.Source.Link != nil && node.Source.Link.Path != "" {
			dirs[name] = node.Source.Link.Path
		} else if node.Source.Dir != "" {
			dirs[name] = node.Source.Dir
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
		SubParents:    rc.ctx.Resolver.SubParents(),
	}
}
