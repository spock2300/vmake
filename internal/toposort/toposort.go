package toposort

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

func TopologicalSort[N any](nodes map[string]N, getDeps func(N) []string) ([]string, error) {
	inDegree := make(map[string]int, len(nodes))
	dependents := make(map[string][]string)
	for name := range nodes {
		inDegree[name] = 0
	}

	for name, node := range nodes {
		for _, dep := range getDeps(node) {
			if _, exists := nodes[dep]; exists {
				inDegree[name]++
				dependents[dep] = append(dependents[dep], name)
			}
		}
	}

	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)

	result := make([]string, 0, len(nodes))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		for _, dep := range dependents[current] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
		sort.Strings(queue)
	}

	if len(result) != len(nodes) {
		remaining := make(map[string]N)
		for name, node := range nodes {
			if inDegree[name] > 0 {
				remaining[name] = node
			}
		}
		if cycle := FindCycle(remaining, getDeps); len(cycle) > 0 {
			return nil, fmt.Errorf("circular dependency detected: %s", strings.Join(cycle, " -> "))
		}
		names := make([]string, 0, len(remaining))
		for name := range remaining {
			names = append(names, name)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("circular dependency detected: %s", strings.Join(names, " -> "))
	}

	return result, nil
}

func FindCycle[N any](nodes map[string]N, getDeps func(N) []string) []string {
	names := make([]string, 0, len(nodes))
	for name := range nodes {
		names = append(names, name)
	}
	sort.Strings(names)

	color := make(map[string]uint8, len(nodes))
	var stack []string

	var dfs func(u string) []string
	dfs = func(u string) []string {
		color[u] = 1
		stack = append(stack, u)

		rawDeps := getDeps(nodes[u])
		deps := make([]string, 0, len(rawDeps))
		seen := make(map[string]bool, len(rawDeps))
		for _, dep := range rawDeps {
			if _, ok := nodes[dep]; ok && !seen[dep] {
				seen[dep] = true
				deps = append(deps, dep)
			}
		}
		sort.Strings(deps)

		for _, v := range deps {
			if color[v] == 1 {
				idx := slices.Index(stack, v)
				cycle := make([]string, len(stack)-idx+1)
				copy(cycle, stack[idx:])
				cycle[len(cycle)-1] = v
				return cycle
			}
			if color[v] == 0 {
				if cycle := dfs(v); len(cycle) > 0 {
					return cycle
				}
			}
		}

		stack = stack[:len(stack)-1]
		color[u] = 2
		return nil
	}

	for _, name := range names {
		if color[name] == 0 {
			if cycle := dfs(name); len(cycle) > 0 {
				return cycle
			}
		}
	}
	return nil
}
