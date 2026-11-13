package toposort

import (
	"slices"
	"strings"
	"testing"
)

func TestTopologicalSortAcyclic(t *testing.T) {
	nodes := map[string][]string{
		"app":  {"liba", "libb"},
		"liba": {"core"},
		"libb": {"core"},
		"core": nil,
	}
	order, err := TopologicalSort(nodes, func(deps []string) []string { return deps })
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	want := []string{"core", "liba", "libb", "app"}
	if !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestTopologicalSortCycleStripsUpstreamBystanders(t *testing.T) {
	nodes := map[string][]string{
		"0_entry": {"main"},
		"main":    {"a"},
		"a":       {"b", "leaf"},
		"b":       {"c"},
		"c":       {"a"},
		"leaf":    nil,
	}
	_, err := TopologicalSort(nodes, func(deps []string) []string { return deps })
	if err == nil {
		t.Fatal("expected cycle error")
	}
	want := "circular dependency detected: a -> b -> c -> a"
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
	if strings.Contains(err.Error(), "main") || strings.Contains(err.Error(), "0_entry") {
		t.Fatalf("cycle error leaked upstream nodes: %v", err)
	}
}

func TestFindCycleSelfLoop(t *testing.T) {
	nodes := map[string][]string{
		"self": {"self"},
	}
	got := FindCycle(nodes, func(deps []string) []string { return deps })
	want := []string{"self", "self"}
	if !slices.Equal(got, want) {
		t.Fatalf("FindCycle = %v, want %v", got, want)
	}
}
