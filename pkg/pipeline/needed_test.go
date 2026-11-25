package pipeline

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func neededNode(name string, deps ...string) *resolver.PackageNode {
	rel := strings.TrimPrefix(name, "//")
	src := buildscript.NewSource(name, "/src/"+rel+"/build.go", "/src/"+rel, api.SourceLocal)
	node := resolver.NewPackageNode(name, api.LocalContainerID, rel, resolver.SourceKindLocal, src, api.NewPackage().SetName(name))
	node.Deps = append(node.Deps, deps...)
	return node
}

func neededContext(roots []string, nodes ...*resolver.PackageNode) *RuntimeContext {
	r := resolver.NewResolver(nil, "/tmp/deps")
	r.Graph().Roots = roots
	for _, node := range nodes {
		r.Graph().Packages[node.ID] = node
		r.Graph().Order = append(r.Graph().Order, node.ID)
	}
	return &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Config: emptyConfig()}
}

func TestComputeNeededRootSelection(t *testing.T) {
	t.Run("manifest roots win", func(t *testing.T) {
		app := neededNode("//app", "//lib")
		lib := neededNode("//lib")
		extra := neededNode("//extra")
		needed, err := ComputeNeeded(neededContext([]string{"//app"}, app, lib, extra))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(needed.Roots, []string{"//app"}) {
			t.Fatalf("roots = %v, want [//app]", needed.Roots)
		}
		if !needed.Set["//app"] || !needed.Set["//lib"] || needed.Set["//extra"] {
			t.Fatalf("needed = %v", needed.Set)
		}
	})

	t.Run("config root overrides manifest roots", func(t *testing.T) {
		app := neededNode("//app", "//lib")
		lib := neededNode("//lib")
		ctx := neededContext([]string{"//app"}, app, lib)
		ctx.Config = &config.ConfigFile{Root: "//lib"}
		needed, err := ComputeNeeded(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(needed.Roots, []string{"//lib"}) {
			t.Fatalf("roots = %v, want [//lib]", needed.Roots)
		}
		if needed.Set["//app"] || !needed.Set["//lib"] {
			t.Fatalf("needed = %v", needed.Set)
		}
	})

	t.Run("config roots array overrides manifest roots", func(t *testing.T) {
		a := neededNode("//a")
		b := neededNode("//b")
		c := neededNode("//c")
		ctx := neededContext([]string{"//a"}, a, b, c)
		ctx.Config = &config.ConfigFile{Roots: []string{"//b", "//c"}}
		needed, err := ComputeNeeded(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(needed.Roots, []string{"//b", "//c"}) {
			t.Fatalf("roots = %v, want [//b //c]", needed.Roots)
		}
	})

	t.Run("non-local config root is rejected", func(t *testing.T) {
		app := neededNode("//app")
		remote := resolver.NewPackageNode("hl/sdk/sub", "hl/sdk", "sub", resolver.SourceKindNative,
			buildscript.NewSource("hl/sdk/sub", "/r/build.go", "/r", api.SourceRemote), api.NewPackage())
		ctx := neededContext([]string{"//app"}, app, remote)
		ctx.Config = &config.ConfigFile{Root: "hl/sdk/sub"}
		if _, err := ComputeNeeded(ctx); err == nil {
			t.Fatal("remote config root was accepted")
		}
	})

	t.Run("fallback without explicit roots uses unreferenced locals", func(t *testing.T) {
		app := neededNode("//app", "//lib")
		lib := neededNode("//lib")
		needed, err := ComputeNeeded(neededContext(nil, app, lib))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(needed.Roots, []string{"//app"}) {
			t.Fatalf("roots = %v, want [//app]", needed.Roots)
		}
	})
}

func TestNeededSetQueries(t *testing.T) {
	set := NeededSet{"//lib": true, "//app": true}
	if !set.Has("//app") || set.Has("//missing") {
		t.Fatalf("Has = %v/%v", set.Has("//app"), set.Has("//missing"))
	}
	if set.Len() != 2 {
		t.Fatalf("Len = %d, want 2", set.Len())
	}
	if got := set.Names(); !reflect.DeepEqual(got, []string{"//app", "//lib"}) {
		t.Fatalf("Names = %v, want sorted names", got)
	}
	var nilSet NeededSet
	if nilSet.Has("//app") || nilSet.Len() != 0 || len(nilSet.Names()) != 0 {
		t.Fatal("nil NeededSet must behave as an empty set")
	}
}
