package build

import (
	"fmt"
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/internal/toposort"
	"gitee.com/spock2300/vmake/pkg/api"
)

type PkgBuildMeta struct {
	Origin api.SourceOrigin
	Deps   []string
}

func (m PkgBuildMeta) IsRemote() bool {
	return m.Origin == api.SourceRemote
}

type BuildGraph struct {
	Nodes map[string]*BuildNode
	Order []string
}

func (g *BuildGraph) GetNode(name string) (*BuildNode, error) {
	node, ok := g.Nodes[name]
	if !ok {
		return nil, fmt.Errorf("target not found: %s", name)
	}
	return node, nil
}

func (g *BuildGraph) ForEachDefault(fn func(node *BuildNode) error) error {
	for _, fullName := range g.Order {
		node, err := g.GetNode(fullName)
		if err != nil {
			return err
		}
		if !node.Target.IsDefault() {
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
) (*BuildGraph, error) {
	graph := &BuildGraph{
		Nodes: make(map[string]*BuildNode),
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

	for pkgName, pkgTargets := range targets {
		for targetName, target := range pkgTargets {
			fullName := fmt.Sprintf("%s:%s", pkgName, targetName)
			node := graph.Nodes[fullName]

			resolved, err := resolveDeps(target.Deps(), pkgName, graph.Nodes, pkgMeta, nil)
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

func resolveDeps(
	deps []string,
	currentPkg string,
	nodes map[string]*BuildNode,
	pkgMeta map[string]PkgBuildMeta,
	path []string,
) ([]string, error) {
	var result []string
	seen := make(map[string]bool)

	for _, dep := range deps {
		expanded, err := resolveDep(dep, currentPkg, nodes, pkgMeta, path)
		if err != nil {
			return nil, err
		}
		for _, d := range expanded {
			if !seen[d] {
				seen[d] = true
				result = append(result, d)
			}
		}
	}
	return result, nil
}

func resolveDep(
	dep string,
	currentPkg string,
	nodes map[string]*BuildNode,
	pkgMeta map[string]PkgBuildMeta,
	path []string,
) ([]string, error) {
	if strings.Contains(dep, ":") {
		pkgRef, targetSpec, _ := strings.Cut(dep, ":")
		if targetSpec == "*" {
			return resolvePackageRef(pkgRef, nodes, pkgMeta, path)
		}
		if _, exists := nodes[dep]; !exists {
			return nil, fmt.Errorf("dependency not found: %s", dep)
		}
		return []string{dep}, nil
	}

	if strings.Contains(dep, "/") {
		return resolvePackageRef(dep, nodes, pkgMeta, path)
	}

	qualified := currentPkg + ":" + dep
	if _, exists := nodes[qualified]; !exists {
		return nil, fmt.Errorf("dependency not found: %s", dep)
	}
	return []string{qualified}, nil
}

func resolvePackageRef(
	pkgRef string,
	nodes map[string]*BuildNode,
	pkgMeta map[string]PkgBuildMeta,
	path []string,
) ([]string, error) {
	if err := api.CheckCycle(path, pkgRef); err != nil {
		return nil, err
	}

	var result []string

	hasMeta := false
	if meta, ok := pkgMeta[pkgRef]; ok {
		hasMeta = true
		for _, transDep := range meta.Deps {
			expanded, err := resolvePackageRef(transDep, nodes, pkgMeta, append(path, pkgRef))
			if err != nil {
				return nil, err
			}
			result = append(result, expanded...)
		}
	}

	pkgTargetNodes := findPackageTargetNodes(pkgRef, nodes)
	if len(pkgTargetNodes) == 0 && !hasMeta {
		return nil, fmt.Errorf("package not found in build graph: %s", pkgRef)
	}
	result = append(result, pkgTargetNodes...)

	return result, nil
}

func findPackageTargetNodes(pkgName string, nodes map[string]*BuildNode) []string {
	var result []string
	prefix := pkgName + ":"
	for fullName := range nodes {
		if strings.HasPrefix(fullName, prefix) {
			result = append(result, fullName)
		}
	}
	sort.Strings(result)
	return result
}

func topologicalSort(nodes map[string]*BuildNode) ([]string, error) {
	return toposort.TopologicalSort(nodes, func(n *BuildNode) []string { return n.Deps })
}
