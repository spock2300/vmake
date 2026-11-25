package pipeline

import (
	"fmt"
	"sort"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/resolver"
)

// NeededSet is the set of packages reachable from the active roots. It is a
// named map so every signature that carries it says what the map means.
type NeededSet map[string]bool

// Clone returns an independent copy of the set.
func (s NeededSet) Clone() NeededSet {
	if s == nil {
		return nil
	}
	out := make(NeededSet, len(s))
	for name := range s {
		out[name] = true
	}
	return out
}

func (s NeededSet) Has(name string) bool { return s[name] }
func (s NeededSet) Len() int             { return len(s) }

// Names returns the set members in sorted order.
func (s NeededSet) Names() []string {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Needed is the single reachable-package computation used by build, inspect,
// query, deps and test preflight.
type Needed struct {
	Set       NeededSet
	Roots     []string
	Rationale string
}

func computeNeededGraph(graph *resolver.Graph, rootOverrides []string) (Needed, error) {
	roots, err := computeRootSeeds(graph, rootOverrides)
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

// NeededContainers returns the external container IDs referenced by any package
// in needed.
func NeededContainers(graph *resolver.Graph, needed NeededSet) map[string]bool {
	out := make(map[string]bool)
	if graph == nil {
		return out
	}
	for pkgID := range needed {
		node := graph.Packages[pkgID]
		if node == nil || node.Container == "" || node.Container == api.LocalContainerID {
			continue
		}
		out[node.Container] = true
	}
	return out
}

// ComputeNeeded is the exported entry point for commands that need the same
// package set as RunBuild.
func ComputeNeeded(ctx *RuntimeContext) (Needed, error) {
	if ctx == nil || ctx.DepGraph == nil {
		return Needed{}, fmt.Errorf("project dependency graph is not initialized")
	}
	return computeNeededGraph(ctx.DepGraph, configRootOverrides(ctx))
}

// RootsFromEdges reports the graph roots for a package/edge view. Build
// reachability uses computeNeededGraph (AddRequires/Deps); this helper serves
// the reported deps/inspect graph, whose edges also include target-level
// AddDeps/UseRequires. Both consumers share it instead of sorting edges twice.
func RootsFromEdges(ctx *RuntimeContext, pkgs []string, edges []PackageEdge, needed NeededSet) []string {
	if ctx == nil || ctx.DepGraph == nil {
		return nil
	}
	if overrides := configRootOverrides(ctx); len(overrides) > 0 {
		var roots []string
		for _, root := range overrides {
			if node := ctx.DepGraph.Packages[root]; node != nil && needed[root] && node.IsLocal() {
				roots = append(roots, root)
			}
		}
		if len(roots) > 0 {
			return roots
		}
	}
	if len(ctx.DepGraph.Roots) > 0 {
		var roots []string
		for _, root := range ctx.DepGraph.Roots {
			if node := ctx.DepGraph.Packages[root]; node != nil && needed[root] && node.IsLocal() {
				roots = append(roots, root)
			}
		}
		if len(roots) > 0 {
			return roots
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
