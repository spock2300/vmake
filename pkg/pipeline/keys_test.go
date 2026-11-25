package pipeline

import (
	"reflect"
	"testing"

	"github.com/spock2300/vmake/pkg/resolver"
)

func TestNeededContainersCollectsExternalContainersOnly(t *testing.T) {
	graph := resolver.NewGraph()
	graph.Packages["//app"] = &resolver.PackageNode{ID: "//app", Container: "//", Kind: resolver.SourceKindLocal}
	graph.Packages["hl/sdk/chip"] = &resolver.PackageNode{ID: "hl/sdk/chip", Container: "hl/sdk", Kind: resolver.SourceKindNative}
	graph.Packages["hl/sdk/board"] = &resolver.PackageNode{ID: "hl/sdk/board", Container: "hl/sdk", Kind: resolver.SourceKindNative}
	graph.Packages["official/tinyexpr"] = &resolver.PackageNode{ID: "official/tinyexpr", Container: "official", Kind: resolver.SourceKindRegistry}
	graph.Packages["unused/mod/x"] = &resolver.PackageNode{ID: "unused/mod/x", Container: "unused/mod", Kind: resolver.SourceKindNative}

	needed := NeededSet{
		"//app":             true,
		"hl/sdk/chip":       true,
		"hl/sdk/board":      true,
		"official/tinyexpr": true,
	}
	got := NeededContainers(graph, needed)
	want := map[string]bool{
		"hl/sdk":   true,
		"official": true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NeededContainers = %v, want %v", got, want)
	}
}
