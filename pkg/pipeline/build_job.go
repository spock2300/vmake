package pipeline

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

func buildEnvironment(rc *runContext, cfg *configStage, src *sourceStage, decl *declareStage) *build.BuildEnv {
	packages := make(map[string]*api.Package)
	for _, name := range rc.ctx.Resolver.GetOrder() {
		node := rc.ctx.DepGraph.Packages[name]
		if cfg.needed[name] && node != nil && node.Pkg != nil {
			packages[name] = node.Pkg
		}
	}
	return &build.BuildEnv{
		Layouts:     src.layouts,
		Packages:    packages,
		Toolchains:  decl.packageToolchains(),
		Options:     cfg.allPkgOptions,
		GlobalFlags: rc.globalFlags,
		Session:     rc.session,
		RootDir:     rc.ctx.Paths.ProjectDir,
		Jobs:        rc.opts.Jobs,
		KeepGoing:   rc.opts.KeepGoing,
		SubParents:  rc.ctx.Resolver.SubParents(),
	}
}
