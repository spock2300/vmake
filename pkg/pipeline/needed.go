package pipeline

import (
	"fmt"
	"sort"

	"github.com/spock2300/vmake/pkg/resolver"
)

// Needed is the single reachable-package computation used by build, inspect,
// query, deps and test preflight.
type Needed struct {
	Set       map[string]bool
	Roots     []string
	Rationale string
}

func computeNeededGraph(graph *resolver.Graph, rootOverride string) (Needed, error) {
	roots, err := computeRootSeeds(graph, rootOverride)
	if err != nil {
		return Needed{}, err
	}
	set := make(map[string]bool, len(graph.Packages))
	queue := append([]string(nil), roots...)
	for _, id := range roots {
		set[id] = true
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if node, ok := graph.Packages[name]; ok {
			for _, dep := range node.Deps {
				if !set[dep] {
					set[dep] = true
					queue = append(queue, dep)
				}
			}
		}
	}
	return Needed{Set: set, Roots: roots}, nil
}

// ComputeNeeded is the exported entry point for commands that need the same
// package set as RunBuild.
func ComputeNeeded(ctx *RuntimeContext) (Needed, error) {
	if ctx == nil || ctx.DepGraph == nil {
		return Needed{}, fmt.Errorf("project dependency graph is not initialized")
	}
	return computeNeededGraph(ctx.DepGraph, configRootOverride(ctx))
}

// RootsFromEdges reports the graph roots for a package/edge view. Build
// reachability uses computeNeededGraph (AddRequires/Deps); this helper serves
// the reported deps/inspect graph, whose edges also include target-level
// AddDeps/UseRequires. Both consumers share it instead of sorting edges twice.
func RootsFromEdges(ctx *RuntimeContext, pkgs []string, edges []PackageEdge, needed map[string]bool) []string {
	if ctx == nil || ctx.DepGraph == nil {
		return nil
	}
	if rootOverride := configRootOverride(ctx); rootOverride != "" {
		if node := ctx.DepGraph.Packages[rootOverride]; node != nil && needed[rootOverride] && node.IsLocal() {
			return []string{rootOverride}
		}
	}
	for _, name := range pkgs {
		node := ctx.DepGraph.Packages[name]
		if node != nil && needed[name] && node.IsLocal() && node.Pkg != nil && node.Pkg.IsRoot() {
			return []string{name}
		}
	}
	dependedOn := make(map[string]bool)
	for _, edge := range edges {
		if needed[edge.From] && needed[edge.To] {
			dependedOn[edge.To] = true
		}
	}
	roots := make([]string, 0)
	for _, name := range pkgs {
		node := ctx.DepGraph.Packages[name]
		if node == nil || !needed[name] || !node.IsLocal() || dependedOn[name] {
			continue
		}
		roots = append(roots, name)
	}
	if len(roots) == 0 {
		for _, name := range pkgs {
			node := ctx.DepGraph.Packages[name]
			if node != nil && needed[name] && node.IsLocal() {
				roots = append(roots, name)
			}
		}
	}
	sort.Strings(roots)
	return roots
}
