package build

import (
	"fmt"
	"os"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type SubGraphParams struct {
	AllTargets map[string]map[string]*api.Target
	PkgMeta    map[string]PkgBuildMeta
	PkgDirs    map[string]*api.PkgDirs
	Packages   map[string]*api.Package
	Needed     map[string]bool
	SubParents map[string]string
}

func CollectSubGraphPackages(rootPkg string, pkgMeta map[string]PkgBuildMeta, allTargets map[string]map[string]*api.Target, needed map[string]bool) map[string]bool {
	visited := make(map[string]bool)
	var visit func(pkgName string)
	visit = func(pkgName string) {
		if visited[pkgName] {
			return
		}
		visited[pkgName] = true

		meta, ok := pkgMeta[pkgName]
		if !ok {
			return
		}
		for _, dep := range meta.Deps {
			if needed[dep] {
				visit(dep)
			}
		}

		targets, ok := allTargets[pkgName]
		if !ok {
			return
		}
		for _, t := range targets {
			for _, dep := range t.Deps() {
				depPkg := dep
				if idx := strings.Index(dep, ":"); idx >= 0 {
					depPkg = dep[:idx]
				}
				if needed[depPkg] {
					visit(depPkg)
				}
			}
		}
	}

	visit(rootPkg)
	return visited
}

func filterMap[V any](src map[string]V, keys map[string]bool) map[string]V {
	dst := make(map[string]V, len(keys))
	for k := range keys {
		if v, ok := src[k]; ok {
			dst[k] = v
		}
	}
	return dst
}

func BuildSubGraph(rootPkg string, tc *toolchain.Toolchain, tcName string, mode string, params *SubGraphParams, pkgOptions map[string]map[string]any) error {
	subPkgs := CollectSubGraphPackages(rootPkg, params.PkgMeta, params.AllTargets, params.Needed)

	if len(subPkgs) == 0 {
		return fmt.Errorf("package %s not found", rootPkg)
	}

	filteredTargets := filterMap(params.AllTargets, subPkgs)
	filteredPkgMeta := filterMap(params.PkgMeta, subPkgs)
	filteredPkgDirs := filterMap(params.PkgDirs, subPkgs)

	vlog.Info("")
	vlog.Info("[subgraph] Building %s (toolchain=%s, mode=%s)", rootPkg, tcName, mode)

	for pkgName := range subPkgs {
		defaultMark := ""
		if meta, ok := filteredPkgMeta[pkgName]; ok && meta.IsRemote() {
			defaultMark = " (remote)"
		}
		vlog.Info("  [subgraph] %s%s", pkgName, defaultMark)
	}

	graph, err := NewBuildGraph(filteredTargets, filteredPkgMeta, params.SubParents)
	if err != nil {
		return fmt.Errorf("subgraph build graph: %w", err)
	}

	pipeline := NewBuildPipeline(graph, tc, filteredPkgDirs, mode, filterMap(pkgOptions, subPkgs), nil)

	for pkgName := range subPkgs {
		if pkg, ok := params.Packages[pkgName]; ok {
			pkg.SetToolchain(tc)
			pipeline.SetPackage(pkgName, pkg)
		}
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	if _, err := pipeline.Run(); err != nil {
		return fmt.Errorf("subgraph build %s: %w", rootPkg, err)
	}

	vlog.Info("  [subgraph] %s done", rootPkg)
	return nil
}

func TargetOutputPath(pkgDir, buildKey string, kind api.TargetKind, targetName string) string {
	filename := targetFilename(kind, targetName)
	return BuildPath(pkgDir, buildKey, filename)
}
