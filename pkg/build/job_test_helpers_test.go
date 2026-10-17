package build

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func testBuildEnv(dirs map[string]*api.PkgDirs, packages map[string]*api.Package, toolchains map[string]*toolchain.Toolchain, session *Session, rootDir string) *BuildEnv {
	layouts := make(map[string]*PackageLayout, len(dirs))
	for name, d := range dirs {
		if d == nil {
			continue
		}
		layouts[name] = &PackageLayout{
			Dirs:     *d,
			BuildKey: "test-key-" + name,
			Ready:    true,
		}
	}
	if packages == nil {
		packages = make(map[string]*api.Package)
	}
	if toolchains == nil {
		toolchains = make(map[string]*toolchain.Toolchain)
	}
	return &BuildEnv{
		Layouts:    layouts,
		Packages:   packages,
		Toolchains: toolchains,
		Session:    session,
		RootDir:    rootDir,
	}
}

func testBuildJob(graph *BuildGraph, tc *toolchain.Toolchain, env *BuildEnv, mode string, platform api.Platform, includeTests bool) *BuildJob {
	return &BuildJob{
		Env:          env,
		Graph:        graph,
		Toolchain:    tc,
		Platform:     platform,
		Mode:         mode,
		IncludeTests: includeTests,
	}
}

func testBuildPackages(dirs map[string]*api.PkgDirs) map[string]*api.Package {
	packages := make(map[string]*api.Package, len(dirs))
	for name, d := range dirs {
		if d != nil {
			packages[name] = api.NewPackage().SetDirs(*d)
		}
	}
	return packages
}
