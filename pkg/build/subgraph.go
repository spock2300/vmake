package build

import (
	"fmt"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

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

type SubGraphRequest struct {
	RootPkg string
	Targets map[string]map[string]*api.Target
	PkgMeta map[string]PkgBuildMeta
	Needed  map[string]bool
}

func BuildSubGraph(base *BuildJob, req SubGraphRequest) error {
	if base == nil || base.Env == nil {
		return fmt.Errorf("subgraph has no build environment")
	}
	if base.Toolchain == nil {
		return fmt.Errorf("subgraph has no toolchain")
	}
	subPkgs := CollectSubGraphPackages(req.RootPkg, req.PkgMeta, req.Targets, req.Needed)
	if len(subPkgs) == 0 {
		return fmt.Errorf("package %s not found", req.RootPkg)
	}
	targets := filterMap(req.Targets, subPkgs)
	pkgMeta := filterMap(req.PkgMeta, subPkgs)
	vlog.Info("")
	vlog.Info("[subgraph] Building %s (toolchain=%s, mode=%s)", req.RootPkg, base.Toolchain.Name, base.Mode)
	for pkgName := range subPkgs {
		defaultMark := ""
		if meta, ok := pkgMeta[pkgName]; ok && meta.IsRemote() {
			defaultMark = " (remote)"
		}
		vlog.Info("  [subgraph] %s%s", pkgName, defaultMark)
	}
	graph, err := NewBuildGraph(targets, pkgMeta, base.Env.SubParents)
	if err != nil {
		return fmt.Errorf("subgraph build graph: %w", err)
	}
	sub := *base
	sub.Graph = graph
	if _, err := sub.Run(); err != nil {
		return fmt.Errorf("subgraph build %s: %w", req.RootPkg, err)
	}
	vlog.Info("  [subgraph] %s done", req.RootPkg)
	return nil
}

func TargetOutputPath(pkgDir, buildKey string, kind api.TargetKind, targetName, targetOS string) string {
	filename := targetFilename(kind, targetName, targetOS)
	return BuildPath(pkgDir, buildKey, filename)
}
