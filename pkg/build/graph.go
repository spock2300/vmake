package build

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/toposort"
	"github.com/spock2300/vmake/pkg/api"
)

type PkgBuildMeta struct {
	Origin api.SourceOrigin
	Deps   []string
}

func (m PkgBuildMeta) IsRemote() bool {
	return m.Origin == api.SourceRemote
}

type BuildGraph struct {
	Nodes   map[string]*BuildNode
	Order   []string
	PkgMeta map[string]PkgBuildMeta
}

func (g *BuildGraph) GetNode(name string) (*BuildNode, error) {
	node, ok := g.Nodes[name]
	if !ok {
		return nil, fmt.Errorf("target not found: %s", name)
	}
	return node, nil
}

func (g *BuildGraph) ForEachDefault(includeTests bool, fn func(node *BuildNode) error) error {
	for _, fullName := range g.Order {
		node, err := g.GetNode(fullName)
		if err != nil {
			return err
		}
		if !node.Target.IsDefault() {
			continue
		}
		if node.Target.IsTest() && !includeTests {
			continue
		}
		if err := fn(node); err != nil {
			return err
		}
	}
	return nil
}

type BuildNode struct {
	FullName string
	PkgName  string
	Target   *api.Target
	Deps     []string
}

func NewBuildGraph(
	targets map[string]map[string]*api.Target,
	pkgMeta map[string]PkgBuildMeta,
	subParents map[string]string,
) (*BuildGraph, error) {
	graph := &BuildGraph{
		Nodes:   make(map[string]*BuildNode),
		PkgMeta: pkgMeta,
	}

	for pkgName, pkgTargets := range targets {
		for targetName, target := range pkgTargets {
			fullName := fmt.Sprintf("%s:%s", pkgName, targetName)
			graph.Nodes[fullName] = &BuildNode{
				FullName: fullName,
				PkgName:  pkgName,
				Target:   target,
				Deps:     make([]string, 0),
			}
		}
	}

	resolver := newDepResolver(graph.Nodes, pkgMeta, subParents)

	for pkgName, pkgTargets := range targets {
		for targetName, target := range pkgTargets {
			fullName := fmt.Sprintf("%s:%s", pkgName, targetName)
			node := graph.Nodes[fullName]

			resolved, err := resolver.resolveDeps(target.Deps(), pkgName, nil)
			if err != nil {
				return nil, err
			}
			node.Deps = resolved
		}
	}

	order, err := topologicalSort(graph.Nodes)
	if err != nil {
		return nil, err
	}
	graph.Order = order

	return graph, nil
}

type depResolver struct {
	nodes      map[string]*BuildNode
	pkgMeta    map[string]PkgBuildMeta
	subParents map[string]string
	pkgTargets map[string][]string
	memo       map[string][]string
}

func newDepResolver(nodes map[string]*BuildNode, pkgMeta map[string]PkgBuildMeta, subParents map[string]string) *depResolver {
	pkgTargets := make(map[string][]string)
	for fullName := range nodes {
		if pkg, _, ok := strings.Cut(fullName, ":"); ok {
			pkgTargets[pkg] = append(pkgTargets[pkg], fullName)
		}
	}
	for _, names := range pkgTargets {
		sort.Strings(names)
	}
	return &depResolver{
		nodes:      nodes,
		pkgMeta:    pkgMeta,
		subParents: subParents,
		pkgTargets: pkgTargets,
		memo:       make(map[string][]string),
	}
}

func (d *depResolver) resolveDeps(deps []string, currentPkg string, path []string) ([]string, error) {
	var result []string
	seen := make(map[string]bool)

	for _, dep := range deps {
		expanded, err := d.resolveDep(dep, currentPkg, path)
		if err != nil {
			return nil, err
		}
		for _, x := range expanded {
			if !seen[x] {
				seen[x] = true
				result = append(result, x)
			}
		}
	}
	return result, nil
}

func (d *depResolver) resolveDep(dep string, currentPkg string, path []string) ([]string, error) {
	if strings.Contains(dep, ":") {
		pkgRef, targetSpec, _ := strings.Cut(dep, ":")
		pkgRef = d.resolveDepPkgName(currentPkg, pkgRef)
		if targetSpec == "*" {
			return d.resolvePackageRef(pkgRef, path)
		}
		fullDep := pkgRef + ":" + targetSpec
		if _, exists := d.nodes[fullDep]; !exists {
			return nil, fmt.Errorf("dependency not found: %s (resolved: %s)", dep, fullDep)
		}
		return []string{fullDep}, nil
	}

	if strings.Contains(dep, "/") {
		return d.resolvePackageRef(dep, path)
	}

	qualified := currentPkg + ":" + dep
	if _, exists := d.nodes[qualified]; !exists {
		return nil, fmt.Errorf("dependency not found: %s", dep)
	}
	return []string{qualified}, nil
}

func (d *depResolver) resolveDepPkgName(currentPkg, depName string) string {
	return api.ResolveSubPackageName(currentPkg, depName, d.subParents, func(candidate string) bool {
		_, ok := d.pkgMeta[candidate]
		return ok
	})
}

func (d *depResolver) resolvePackageRef(pkgRef string, path []string) ([]string, error) {
	if err := api.CheckCycle(path, pkgRef); err != nil {
		return nil, err
	}

	if cached, ok := d.memo[pkgRef]; ok {
		return cached, nil
	}

	var result []string

	hasMeta := false
	if meta, ok := d.pkgMeta[pkgRef]; ok {
		hasMeta = true
		for _, transDep := range meta.Deps {
			transDep = d.resolveDepPkgName(pkgRef, transDep)
			expanded, err := d.resolvePackageRef(transDep, append(path, pkgRef))
			if err != nil {
				return nil, err
			}
			result = append(result, expanded...)
		}
	}

	pkgTargetNodes := d.pkgTargets[pkgRef]
	if len(pkgTargetNodes) == 0 && !hasMeta {
		return nil, fmt.Errorf("package not found in build graph: %s", pkgRef)
	}
	result = append(result, pkgTargetNodes...)

	d.memo[pkgRef] = result
	return result, nil
}

func topologicalSort(nodes map[string]*BuildNode) ([]string, error) {
	return toposort.TopologicalSort(nodes, func(n *BuildNode) []string { return n.Deps })
}
