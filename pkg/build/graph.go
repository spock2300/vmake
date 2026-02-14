package build

import (
	"fmt"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
)

type BuildGraph struct {
	Nodes map[string]*BuildNode
	Order []string
}

type BuildNode struct {
	FullName string
	PkgName  string
	Target   *api.Target
	Deps     []string
}

func NewBuildGraph(targets map[string]map[string]*api.Target) (*BuildGraph, error) {
	graph := &BuildGraph{
		Nodes: make(map[string]*BuildNode),
	}

	for pkgName, pkgTargets := range targets {
		for targetName, target := range pkgTargets {
			fullName := fmt.Sprintf("%s:%s", pkgName, targetName)
			node := &BuildNode{
				FullName: fullName,
				PkgName:  pkgName,
				Target:   target,
				Deps:     make([]string, 0),
			}

			for _, dep := range target.Deps() {
				var depFullName string
				if strings.Contains(dep, ":") {
					depFullName = dep
				} else {
					depFullName = fmt.Sprintf("%s:%s", pkgName, dep)
				}
				node.Deps = append(node.Deps, depFullName)
			}

			graph.Nodes[fullName] = node
		}
	}

	order, err := topologicalSort(graph.Nodes)
	if err != nil {
		return nil, err
	}
	graph.Order = order

	return graph, nil
}

func topologicalSort(nodes map[string]*BuildNode) ([]string, error) {
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	var result []string

	var visit func(name string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("circular dependency detected involving: %s", name)
		}

		node, exists := nodes[name]
		if !exists {
			return fmt.Errorf("dependency not found: %s", name)
		}

		visiting[name] = true

		for _, dep := range node.Deps {
			if err := visit(dep); err != nil {
				return err
			}
		}

		visiting[name] = false
		visited[name] = true
		result = append(result, name)

		return nil
	}

	for name := range nodes {
		if !visited[name] {
			if err := visit(name); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}
