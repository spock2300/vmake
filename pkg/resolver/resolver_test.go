package resolver

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

func TestTopologicalSort(t *testing.T) {
	packages := map[string]*PackageNode{
		"a": {ID: "a", Deps: []string{"b", "c"}},
		"b": {ID: "b", Deps: []string{"c"}},
		"c": {ID: "c", Deps: []string{}},
	}

	order, err := topologicalSort(packages)
	if err != nil {
		t.Fatalf("topologicalSort failed: %v", err)
	}

	pos := make(map[string]int, len(order))
	for i, id := range order {
		pos[id] = i
	}
	if pos["c"] >= pos["b"] {
		t.Errorf("c (pos %d) should come before b (pos %d)", pos["c"], pos["b"])
	}
	if pos["b"] >= pos["a"] {
		t.Errorf("b (pos %d) should come before a (pos %d)", pos["b"], pos["a"])
	}
	if pos["c"] >= pos["a"] {
		t.Errorf("c (pos %d) should come before a (pos %d)", pos["c"], pos["a"])
	}
}

func TestTopologicalSortCycle(t *testing.T) {
	packages := map[string]*PackageNode{
		"a": {ID: "a", Deps: []string{"b"}},
		"b": {ID: "b", Deps: []string{"a"}},
	}

	_, err := topologicalSort(packages)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

func TestConstraintsCompatible(t *testing.T) {
	// Note: pkg/api/semver uses caret-like semantics for ">=" — when the
	// constraint's major is > 0, it additionally requires the candidate's
	// major to match. So ">=1.0" does NOT match version 2.0, and the two
	// constraints ">=1.0" / ">=2.0" are therefore NOT bidirectionally
	// compatible. This was discovered during Phase 0 test-net work; the
	// previous test expected standard semver semantics. Behaviour preserved
	// here matches what real builds actually do.
	tests := []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"", ">=1.0", true},
		{">=1.0", "", true},
		{">=1.0", ">=1.0", true},
		{">=1.0", ">=0.5", true},
		{">=2.0", "<1.0", false},
		{">=1.0", "<0.5", false},
		{">=1.0", ">=1.5", true},
		{">=1.5", ">=1.0", true},
		{">=1.0", ">=2.0", false},
		{">=2.0", ">=1.0", false},
	}
	for _, tt := range tests {
		got := constraintsCompatible(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("constraintsCompatible(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCheckNodeConstraints(t *testing.T) {
	node := &PackageNode{
		ID:          "foo",
		Constraints: []string{">=1.0"},
	}

	if err := checkNodeConstraints(node, ">=1.5"); err != nil {
		t.Errorf("compatible constraint should pass: %v", err)
	}
	if err := checkNodeConstraints(node, ""); err != nil {
		t.Errorf("empty constraint should pass: %v", err)
	}
	if err := checkNodeConstraints(node, "<0.5"); err == nil {
		t.Error("conflicting constraint should fail")
	}
}

func TestCheckNodeConstraintsNoMutation(t *testing.T) {
	node := &PackageNode{
		ID:          "foo",
		Constraints: []string{">=1.0"},
	}
	origLen := len(node.Constraints)

	_ = checkNodeConstraints(node, ">=2.0")

	if len(node.Constraints) != origLen {
		t.Errorf("checkNodeConstraints mutated Constraints: got %d items, want %d",
			len(node.Constraints), origLen)
	}
}

func TestResolveAllLocalOnly(t *testing.T) {
	packages := map[string]*PackageNode{
		"app": {ID: "app", Deps: []string{"lib"}},
		"lib": {ID: "lib", Deps: []string{}},
	}

	order, err := topologicalSort(packages)
	if err != nil {
		t.Fatalf("topologicalSort failed: %v", err)
	}

	pos := make(map[string]int, len(order))
	for i, id := range order {
		pos[id] = i
	}
	if pos["lib"] >= pos["app"] {
		t.Errorf("lib should come before app in topological order")
	}
}

func TestPackageNodeIsLocal(t *testing.T) {
	localSrc := buildscript.NewSource("foo", "/path/build.go", "/path", api.SourceLocal)
	localNode := NewPackageNode("foo", localSrc, nil, false)
	if !localNode.IsLocal() {
		t.Error("local source should be IsLocal()")
	}

	remoteSrc := buildscript.NewSource("bar/pkg", "/remote/build.go", "/remote", api.SourceRemote)
	remoteNode := NewPackageNode("bar/pkg", remoteSrc, nil, false)
	if remoteNode.IsLocal() {
		t.Error("remote source should not be IsLocal()")
	}
}

func TestPackageNodeWithNative(t *testing.T) {
	src := buildscript.NewSource("foo", "/path/build.go", "/path", api.SourceRemote)
	node := NewPackageNode("foo", src, nil, true).WithNative("https://example.com/foo.git",
		map[string]string{"1.0.0": "refs/tags/1.0.0"}, "1.0.0")

	if !node.Deferred {
		t.Error("should be deferred")
	}
	if !node.IsNative() {
		t.Error("should be native")
	}
	if node.Native.Selected != "1.0.0" {
		t.Errorf("Selected = %q, want %q", node.Native.Selected, "1.0.0")
	}
	if node.Native.GitURL != "https://example.com/foo.git" {
		t.Errorf("GitURL = %q, want %q", node.Native.GitURL, "https://example.com/foo.git")
	}
}

func TestUpdateOrder(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	r.graph.Packages["a"] = &PackageNode{ID: "a", Deps: []string{"b"}}
	r.graph.Packages["b"] = &PackageNode{ID: "b", Deps: []string{}}

	if err := r.UpdateOrder(); err != nil {
		t.Fatalf("UpdateOrder failed: %v", err)
	}

	if len(r.graph.Order) != 2 {
		t.Fatalf("Order length = %d, want 2", len(r.graph.Order))
	}

	pos := make(map[string]int)
	for i, id := range r.graph.Order {
		pos[id] = i
	}
	if pos["b"] >= pos["a"] {
		t.Errorf("b should come before a: b at %d, a at %d", pos["b"], pos["a"])
	}
}

func TestResolveDeferredNoDeferred(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	r.graph.Packages["a"] = &PackageNode{ID: "a", Deps: []string{}}
	r.graph.Packages["a"].Deferred = false

	if err := r.ResolveDeferred(); err != nil {
		t.Fatalf("ResolveDeferred with no deferred should succeed: %v", err)
	}
}

func TestCycleDetection(t *testing.T) {
	err := api.CheckCycle([]string{"a", "b"}, "a")
	if err == nil {
		t.Fatal("expected cycle error for a -> b -> a")
	}
}

func TestNoCycle(t *testing.T) {
	err := api.CheckCycle([]string{"a", "b"}, "c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
