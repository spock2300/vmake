package pipeline

import (
	"slices"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestRootsHintSinglePackageNamesIt(t *testing.T) {
	graph := &resolver.Graph{Packages: map[string]*resolver.PackageNode{
		"syntax": localNode("syntax"),
	}}
	hint := rootsHint(graph)
	if hint == "" {
		t.Fatal("single-package project must name its only candidate")
	}
	if !strings.HasSuffix(hint, "syntax") {
		t.Errorf("hint does not end with the package name: %q", hint)
	}
	if strings.HasSuffix(strings.TrimSpace(hint), ":") {
		t.Errorf("hint ends with an empty list: %q", hint)
	}
	needed, err := computeReachable(graph)
	if err != nil {
		t.Fatal(err)
	}
	if !needed["syntax"] {
		t.Errorf("needed = %v, want the only local package", needed)
	}
}

func TestRootsHintListsAllLocalPackages(t *testing.T) {
	consumer := localNode("app", "lib")
	consumer.Pkg.OnRequire(func(ctx *api.RequireContext) {})
	graph := &resolver.Graph{Packages: map[string]*resolver.PackageNode{
		"app":        consumer,
		"lib":        localNode("lib"),
		"remote-pkg": remoteNode("remote-pkg"),
	}}
	hint := rootsHint(graph)
	for _, want := range []string{"app", "lib"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint %q missing local package %q", hint, want)
		}
	}
	if strings.Contains(hint, "remote-pkg") {
		t.Errorf("hint %q names a remote package", hint)
	}
	if !strings.HasSuffix(hint, "app, lib") {
		t.Errorf("hint candidates are not sorted: %q", hint)
	}
}

func TestRootsHintSuppressedWithoutLocalPackages(t *testing.T) {
	graph := &resolver.Graph{Packages: map[string]*resolver.PackageNode{
		"remote-pkg": remoteNode("remote-pkg"),
	}}
	if hint := rootsHint(graph); hint != "" {
		t.Fatalf("rootsHint() = %q, want no hint without local candidates", hint)
	}
}

func TestRootsHintCandidateOrderIsDeterministic(t *testing.T) {
	graph := &resolver.Graph{Packages: map[string]*resolver.PackageNode{}}
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		graph.Packages[name] = localNode(name)
	}
	want := []string{"alpha", "bravo", "charlie"}
	for i := 0; i < 50; i++ {
		hint := rootsHint(graph)
		candidates := strings.TrimPrefix(hint, "[hint] no package declares SetRoot(true); consider adding 'p.SetRoot(true)' to one of: ")
		if got := strings.Split(candidates, ", "); !slices.Equal(got, want) {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
}
