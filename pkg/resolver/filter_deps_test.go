package resolver

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestFilterDepsNoPackage(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	r.graph.Packages["//foo"] = &PackageNode{ID: "//foo", Container: "//", Deps: []string{"//old"}}

	err := r.FilterDeps("//foo", nil, nil)
	if err != nil {
		t.Errorf("FilterDeps on nil Pkg should be no-op, got %v", err)
	}
	if _, exists := r.graph.Packages["//foo"]; !exists {
		t.Error("node should still exist")
	}
}

func TestFilterDepsMissingNode(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	err := r.FilterDeps("//nonexistent", nil, nil)
	if err == nil {
		t.Error("FilterDeps on missing node should error")
	}
}

func TestFilterDepsNoRequireFuncs(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	pkg := api.NewPackage()
	r.graph.Packages["//foo"] = &PackageNode{ID: "//foo", Container: "//", Pkg: pkg, Deps: []string{"//old-dep"}}

	if err := r.FilterDeps("//foo", nil, nil); err != nil {
		t.Errorf("FilterDeps with no requireFuncs should be no-op: %v", err)
	}
	if len(r.graph.Packages["//foo"].Deps) != 1 || r.graph.Packages["//foo"].Deps[0] != "//old-dep" {
		t.Errorf("Deps should be unchanged, got %v", r.graph.Packages["//foo"].Deps)
	}
}

func TestFilterDepsReplacesNodeDeps(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	pkg := api.NewPackage()
	called := 0
	pkg.OnRequire(func(ctx *api.RequireContext) {
		called++
		ctx.AddRequires("//real-dep-1", "//real-dep-2")
	})
	api.BindPackageUpdateRequireContext(pkg, nil, nil)

	r.graph.Packages["//foo"] = &PackageNode{ID: "//foo", Container: "//", Kind: SourceKindLocal, Pkg: pkg, Deps: []string{"//old-dep"}}
	r.graph.Packages["//real-dep-1"] = &PackageNode{ID: "//real-dep-1", Container: "//", Kind: SourceKindLocal}
	r.graph.Packages["//real-dep-2"] = &PackageNode{ID: "//real-dep-2", Container: "//", Kind: SourceKindLocal}

	if err := r.FilterDeps("//foo", nil, nil); err != nil {
		t.Fatalf("FilterDeps: %v", err)
	}
	if called == 0 {
		t.Error("FilterDeps should re-run OnRequire via UpdateRequireContext")
	}

	deps := r.graph.Packages["//foo"].Deps
	if len(deps) != 2 {
		t.Fatalf("Deps = %v, want 2 entries", deps)
	}
	if deps[0] != "//real-dep-1" || deps[1] != "//real-dep-2" {
		t.Errorf("Deps = %v, want [//real-dep-1 //real-dep-2]", deps)
	}
}

func TestFilterDepsCanonicalizesContainerRelativeRef(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	pkg := api.NewPackage()
	pkg.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("//sub_b")
	})
	api.BindPackageUpdateRequireContext(pkg, nil, nil)

	r.graph.Packages["hl/sdk/sub_a"] = &PackageNode{ID: "hl/sdk/sub_a", Container: "hl/sdk", Kind: SourceKindNative, Pkg: pkg}
	r.graph.Packages["hl/sdk/sub_b"] = &PackageNode{ID: "hl/sdk/sub_b", Container: "hl/sdk", Kind: SourceKindNative}

	if err := r.FilterDeps("hl/sdk/sub_a", nil, nil); err != nil {
		t.Fatalf("FilterDeps: %v", err)
	}
	deps := r.graph.Packages["hl/sdk/sub_a"].Deps
	if len(deps) != 1 || deps[0] != "hl/sdk/sub_b" {
		t.Fatalf("Deps = %v, want [hl/sdk/sub_b]", deps)
	}
}

func TestFilterDepsUnknownDepErrors(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	pkg := api.NewPackage()
	pkg.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("//ghost-dep")
	})
	api.BindPackageUpdateRequireContext(pkg, nil, nil)

	r.graph.Packages["//foo"] = &PackageNode{ID: "//foo", Container: "//", Kind: SourceKindLocal, Pkg: pkg, Deps: []string{}}

	err := r.FilterDeps("//foo", nil, nil)
	if err == nil {
		t.Fatal("FilterDeps with unknown dep name should error")
	}
	if !strings.Contains(err.Error(), "//ghost-dep") {
		t.Errorf("error should mention the unknown dep, got: %v", err)
	}
}

func TestValidateSourceNodeConstraintsConflictFatal(t *testing.T) {
	pkg := api.NewPackage().SetRepo("official").SetName("zlib")
	pkg.SetGit("https://example.invalid/zlib.git")
	pkg.SetVersions(map[string]string{
		"1.2.0": "v1.2.0",
		"1.5.0": "v1.5.0",
		"1.9.0": "v1.9.0",
	})
	r := NewResolver(nil, t.TempDir())
	node := &PackageNode{
		ID:          "official/zlib",
		Container:   "official",
		Kind:        SourceKindRegistry,
		Constraints: []string{">=1.2", ">=1.5"},
		Pkg:         pkg,
	}
	if err := r.validateSourceNodeConstraints(node); err != nil {
		t.Errorf("compatible constraints should pass: %v", err)
	}
	node.Constraints = append(node.Constraints, "<1.5")
	err := r.validateSourceNodeConstraints(node)
	if err == nil {
		t.Fatal("conflicting constraints with no common version should fail")
	}
	if !strings.Contains(err.Error(), "official/zlib") {
		t.Errorf("error should mention package, got: %v", err)
	}
}

func TestGraphOrderingDeterministic(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	packages := map[string]*PackageNode{
		"//c": {ID: "//c", Deps: []string{}},
		"//b": {ID: "//b", Deps: []string{"//c"}},
		"//a": {ID: "//a", Deps: []string{"//b", "//c"}},
		"//d": {ID: "//d", Deps: []string{}},
		"//e": {ID: "//e", Deps: []string{"//a", "//d"}},
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
	if pos["//c"] >= pos["//b"] {
		t.Errorf("//c should precede //b (c=%d b=%d)", pos["//c"], pos["//b"])
	}
	if pos["//b"] >= pos["//a"] {
		t.Errorf("//b should precede //a")
	}
	if pos["//a"] >= pos["//e"] {
		t.Errorf("//a should precede //e")
	}
}
