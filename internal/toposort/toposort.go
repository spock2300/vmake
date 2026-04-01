package toposort

import (
	"fmt"
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
		remaining := make([]string, 0)
		for name := range nodes {
			if inDegree[name] > 0 {
				remaining = append(remaining, name)
			}
		}
		sort.Strings(remaining)
		return nil, fmt.Errorf("circular dependency detected involving: %s", strings.Join(remaining, ", "))
	}

	return result, nil
}
