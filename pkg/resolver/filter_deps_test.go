package resolver

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

func TestResolveDepNameBareWithNoSubParents(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	r.sources["foo"] = buildscript.NewSource("foo", "/p/build.go", "/p", api.SourceLocal)

	got := r.resolveDepName("callingPkg", "foo")
	if got != "foo" {
		t.Errorf("resolveDepName = %q, want foo", got)
	}
}

func TestResolveDepNamePassesThroughAbsolute(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	r.sources["parent/child"] = buildscript.NewSource("parent/child", "/p/build.go", "/p", api.SourceLocal)
	r.subParents["parent/child"] = "parent"

	got := r.resolveDepName("parent/child", "parent/child")
	if got != "parent/child" {
		t.Errorf("absolute name should pass through, got %q", got)
	}
}

func TestResolveDepNameSubPackageResolution(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	r.sources["parent/sub"] = buildscript.NewSource("parent/sub", "/p/build.go", "/p", api.SourceLocal)
	r.sources["parent/dep"] = buildscript.NewSource("parent/dep", "/p2/build.go", "/p2", api.SourceLocal)
	r.subParents["parent/sub"] = "parent"

	got := r.resolveDepName("parent/sub", "dep")
	if got != "parent/dep" {
		t.Errorf("sub-package resolution = %q, want parent/dep", got)
	}
}

func TestFilterDepsNoPackage(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	r.graph.Packages["foo"] = &PackageNode{ID: "foo", Deps: []string{"old"}}

	err := r.FilterDeps("foo", nil, nil)
	if err != nil {
		t.Errorf("FilterDeps on nil Pkg should be no-op, got %v", err)
	}
	if _, exists := r.graph.Packages["foo"]; !exists {
		t.Error("node should still exist")
	}
}

func TestFilterDepsMissingNode(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	err := r.FilterDeps("nonexistent", nil, nil)
	if err == nil {
		t.Error("FilterDeps on missing node should error")
	}
}

func TestFilterDepsNoRequireFuncs(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	pkg := api.NewPackage()
	r.graph.Packages["foo"] = &PackageNode{ID: "foo", Pkg: pkg, Deps: []string{"old-dep"}}

	if err := r.FilterDeps("foo", nil, nil); err != nil {
		t.Errorf("FilterDeps with no requireFuncs should be no-op: %v", err)
	}
	if len(r.graph.Packages["foo"].Deps) != 1 || r.graph.Packages["foo"].Deps[0] != "old-dep" {
		t.Errorf("Deps should be unchanged, got %v", r.graph.Packages["foo"].Deps)
	}
}

func TestFilterDepsReplacesNodeDeps(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	pkg := api.NewPackage()
	called := 0
	pkg.OnRequire(func(ctx *api.RequireContext) {
		called++
		ctx.AddRequires("real-dep-1", "real-dep-2")
	})
	pkg.UpdateRequireContext(nil, nil)

	r.graph.Packages["foo"] = &PackageNode{ID: "foo", Pkg: pkg, Deps: []string{"old-dep"}}
	r.sources["real-dep-1"] = buildscript.NewSource("real-dep-1", "/p1/build.go", "/p1", api.SourceLocal)
	r.sources["real-dep-2"] = buildscript.NewSource("real-dep-2", "/p2/build.go", "/p2", api.SourceLocal)

	if err := r.FilterDeps("foo", nil, nil); err != nil {
		t.Fatalf("FilterDeps: %v", err)
	}
	if called == 0 {
		t.Error("FilterDeps should re-run OnRequire via UpdateRequireContext")
	}

	deps := r.graph.Packages["foo"].Deps
	if len(deps) != 2 {
		t.Fatalf("Deps = %v, want 2 entries", deps)
	}
	if deps[0] != "real-dep-1" || deps[1] != "real-dep-2" {
		t.Errorf("Deps = %v, want [real-dep-1 real-dep-2]", deps)
	}
}

func TestCheckNodeConstraintsEmptyIncoming(t *testing.T) {
	node := &PackageNode{ID: "x", Constraints: []string{">=1.0"}}
	if err := checkNodeConstraints(node, ""); err != nil {
		t.Errorf("empty incoming should pass: %v", err)
	}
}

func TestCheckNodeConstraintsMultipleCompatible(t *testing.T) {
	node := &PackageNode{ID: "x", Constraints: []string{">=1.0", ">=1.5", "<2.0"}}
	if err := checkNodeConstraints(node, ">=1.6"); err != nil {
		t.Errorf("compatible constraint should pass: %v", err)
	}
}

func TestCheckNodeConstraintsConflictFatal(t *testing.T) {
	node := &PackageNode{ID: "x", Constraints: []string{">=2.0"}}
	if err := checkNodeConstraints(node, "<1.0"); err == nil {
		t.Error("conflicting constraint should fail")
	}
}

func TestConstraintsCompatibleBothEmpty(t *testing.T) {
	if !constraintsCompatible("", "") {
		t.Error("both empty should be compatible")
	}
}

func TestConstraintsCompatibleUnparseable(t *testing.T) {
	if !constraintsCompatible("garbage", "garbage") {
		t.Error("identical unparseable should fall back to equality check")
	}
	if constraintsCompatible("garbage", "different") {
		t.Error("different unparseable should not be compatible")
	}
}

func TestResolveAllLocalEmpty(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	if err := r.ResolveAll(nil); err != nil {
		t.Errorf("ResolveAll with no sources should succeed: %v", err)
	}
	if len(r.graph.Order) != 0 {
		t.Errorf("Order = %v, want empty", r.graph.Order)
	}
}

func TestGraphOrderingDeterministic(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	packages := map[string]*PackageNode{
		"c": {ID: "c", Deps: []string{}},
		"b": {ID: "b", Deps: []string{"c"}},
		"a": {ID: "a", Deps: []string{"b", "c"}},
		"d": {ID: "d", Deps: []string{}},
		"e": {ID: "e", Deps: []string{"a", "d"}},
	}
	r.graph.Packages = packages

	if err := r.UpdateOrder(); err != nil {
		t.Fatalf("UpdateOrder: %v", err)
	}

	first := append([]string{}, r.graph.Order...)
	for i := 0; i < 5; i++ {
		if err := r.UpdateOrder(); err != nil {
			t.Fatal(err)
		}
		if len(first) != len(r.graph.Order) {
			t.Fatalf("order length changed: %d vs %d", len(first), len(r.graph.Order))
		}
		for j := range first {
			if first[j] != r.graph.Order[j] {
				t.Errorf("order non-deterministic at pass %d pos %d: %q vs %q",
					i, j, first[j], r.graph.Order[j])
				break
			}
		}
	}

	pos := make(map[string]int, len(r.graph.Order))
	for i, id := range r.graph.Order {
		pos[id] = i
	}
	if pos["c"] >= pos["b"] {
		t.Errorf("c should precede b (c=%d b=%d)", pos["c"], pos["b"])
	}
	if pos["b"] >= pos["a"] {
		t.Errorf("b should precede a")
	}
	if pos["a"] >= pos["e"] {
		t.Errorf("a should precede e")
	}
}
