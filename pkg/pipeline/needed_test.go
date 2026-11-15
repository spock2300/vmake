package pipeline

import (
	"reflect"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func neededNode(name string, deps ...string) *resolver.PackageNode {
	src := buildscript.NewSource(name, "/src/"+name+"/build.go", "/src/"+name, api.SourceLocal)
	node := resolver.NewPackageNode(name, src, api.NewPackage().SetName(name))
	node.Deps = append(node.Deps, deps...)
	return node
}

func neededContext(nodes ...*resolver.PackageNode) *RuntimeContext {
	r := resolver.NewResolver(nil, "/tmp/deps")
	for _, node := range nodes {
		r.Graph().Packages[node.ID] = node
		r.Graph().Order = append(r.Graph().Order, node.ID)
	}
	return &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Config: emptyConfig()}
}

func TestComputeNeededRootSelection(t *testing.T) {
	t.Run("SetRoot wins", func(t *testing.T) {
		app := neededNode("app", "lib")
		lib := neededNode("lib")
		app.Pkg.SetRoot(true)
		needed, err := ComputeNeeded(neededContext(app, lib))
		if err != nil {
			t.Fatal(err)
		}
		if len(needed.Roots) != 1 || needed.Roots[0] != "app" {
			t.Fatalf("roots = %v, want [app]", needed.Roots)
		}
		if !needed.Set["app"] || !needed.Set["lib"] {
			t.Fatalf("needed = %v", needed.Set)
		}
	})

	t.Run("multiple SetRoot is rejected", func(t *testing.T) {
		a := neededNode("a")
		b := neededNode("b")
		a.Pkg.SetRoot(true)
		b.Pkg.SetRoot(true)
		if _, err := ComputeNeeded(neededContext(a, b)); err == nil {
			t.Fatal("multiple SetRoot packages were accepted")
		}
	})

	t.Run("config root overrides", func(t *testing.T) {
		app := neededNode("app", "lib")
		lib := neededNode("lib")
		ctx := neededContext(app, lib)
		ctx.Config = &config.ConfigFile{Root: "lib"}
		needed, err := ComputeNeeded(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(needed.Roots) != 1 || needed.Roots[0] != "lib" {
			t.Fatalf("roots = %v, want [lib]", needed.Roots)
		}
	})

	t.Run("non-local config root is rejected", func(t *testing.T) {
		app := neededNode("app")
		ctx := neededContext(app)
		ctx.Config = &config.ConfigFile{Root: "remote"}
		if _, err := ComputeNeeded(ctx); err == nil {
			t.Fatal("remote config root was accepted")
		}
	})

	t.Run("OnRequire treats depended-on locals as libraries", func(t *testing.T) {
		app := neededNode("app", "lib")
		lib := neededNode("lib")
		app.Pkg.OnRequire(func(*api.RequireContext) {})
		needed, err := ComputeNeeded(neededContext(app, lib))
		if err != nil {
			t.Fatal(err)
		}
		if len(needed.Roots) != 1 || needed.Roots[0] != "app" {
			t.Fatalf("roots = %v, want [app]", needed.Roots)
		}
	})

	t.Run("no roots still yields local packages", func(t *testing.T) {
		app := neededNode("app")
		lib := neededNode("lib")
		needed, err := ComputeNeeded(neededContext(app, lib))
		if err != nil {
			t.Fatal(err)
		}
		if len(needed.Roots) != 2 || needed.Roots[0] != "app" || needed.Roots[1] != "lib" {
			t.Fatalf("roots = %v, want [app lib]", needed.Roots)
		}
	})
}

func TestNeededSetQueries(t *testing.T) {
	set := NeededSet{"lib": true, "app": true}
	if !set.Has("app") || set.Has("missing") {
		t.Fatalf("Has = %v/%v", set.Has("app"), set.Has("missing"))
	}
	if set.Len() != 2 {
		t.Fatalf("Len = %d, want 2", set.Len())
	}
	if got := set.Names(); !reflect.DeepEqual(got, []string{"app", "lib"}) {
		t.Fatalf("Names = %v, want sorted names", got)
	}
	var nilSet NeededSet
	if nilSet.Has("app") || nilSet.Len() != 0 || len(nilSet.Names()) != 0 {
		t.Fatal("nil NeededSet must behave as an empty set")
	}
}
