package build

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func makeTargetWithDeps(name string, deps ...string) *api.Target {
	tr := api.NewTargetRegistry()
	t := tr.Target(name)
	t.AddDeps(deps...)
	return t
}

func makeTargets(pkg string, targets ...*api.Target) map[string]map[string]*api.Target {
	pkgMap := map[string]*api.Target{}
	for _, t := range targets {
		pkgMap[t.Name()] = t
	}
	return map[string]map[string]*api.Target{pkg: pkgMap}
}

func TestBuildGraphSimpleSamePackageDep(t *testing.T) {
	lib := makeTargetWithDeps("lib")
	app := makeTargetWithDeps("app", "lib")

	graph, err := NewBuildGraph(makeTargets("p", app, lib), nil, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}

	appNode, err := graph.GetNode("p:app")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(appNode.Deps, []string{"p:lib"}) {
		t.Errorf("app deps = %v, want [p:lib]", appNode.Deps)
	}
}

func TestBuildGraphCrossPackageDepExplicit(t *testing.T) {
	lib := makeTargetWithDeps("lib")
	app := makeTargetWithDeps("app")
	app.AddDeps("other:lib")

	targets := map[string]map[string]*api.Target{
		"main":  {"app": app},
		"other": {"lib": lib},
	}
	graph, err := NewBuildGraph(targets, nil, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	appNode, _ := graph.GetNode("main:app")
	if !reflect.DeepEqual(appNode.Deps, []string{"other:lib"}) {
		t.Errorf("cross-package dep = %v", appNode.Deps)
	}
}

func TestBuildGraphPackageRefExpandsAllTargets(t *testing.T) {
	lib1 := makeTargetWithDeps("lib1")
	lib2 := makeTargetWithDeps("lib2")
	app := makeTargetWithDeps("app")
	app.AddDeps("other:*")

	targets := map[string]map[string]*api.Target{
		"main":  {"app": app},
		"other": {"lib1": lib1, "lib2": lib2},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"main":  {Deps: []string{"other"}},
		"other": {},
	}
	graph, err := NewBuildGraph(targets, pkgMeta, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	appNode, _ := graph.GetNode("main:app")
	got := append([]string{}, appNode.Deps...)
	sort.Strings(got)
	want := []string{"other:lib1", "other:lib2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("package-ref expansion = %v, want %v", got, want)
	}
}

func TestBuildGraphWildcardDep(t *testing.T) {
	lib1 := makeTargetWithDeps("lib1")
	lib2 := makeTargetWithDeps("lib2")
	app := makeTargetWithDeps("app")
	app.AddDeps("other:*")

	targets := map[string]map[string]*api.Target{
		"main":  {"app": app},
		"other": {"lib1": lib1, "lib2": lib2},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"main":  {Deps: []string{"other"}},
		"other": {},
	}
	graph, err := NewBuildGraph(targets, pkgMeta, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	appNode, _ := graph.GetNode("main:app")
	got := append([]string{}, appNode.Deps...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"other:lib1", "other:lib2"}) {
		t.Errorf("wildcard dep = %v, want [other:lib1 other:lib2]", got)
	}
}

func TestBuildGraphMissingDep(t *testing.T) {
	app := makeTargetWithDeps("app")
	app.AddDeps("nonexistent")

	_, err := NewBuildGraph(makeTargets("p", app), nil, nil)
	if err == nil {
		t.Error("missing dep should produce error")
	}
}

func TestBuildGraphTopologicalOrder(t *testing.T) {
	lib := makeTargetWithDeps("lib")
	app := makeTargetWithDeps("app", "lib")

	graph, err := NewBuildGraph(makeTargets("p", app, lib), nil, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	pos := make(map[string]int)
	for i, n := range graph.Order {
		pos[n] = i
	}
	if pos["p:lib"] >= pos["p:app"] {
		t.Errorf("lib should come before app: lib=%d app=%d", pos["p:lib"], pos["p:app"])
	}
}

func TestBuildGraphCycleError(t *testing.T) {
	a := makeTargetWithDeps("a", "b")
	b := makeTargetWithDeps("b", "a")
	_, err := NewBuildGraph(makeTargets("p", a, b), nil, nil)
	if err == nil {
		t.Error("cycle should produce error")
	}
}

func TestBuildGraphGetNodeMissing(t *testing.T) {
	graph := &BuildGraph{Nodes: map[string]*BuildNode{}}
	if _, err := graph.GetNode("missing"); err == nil {
		t.Error("GetNode(missing) should error")
	}
}

func TestBuildGraphForEachDefaultSkipsNonDefault(t *testing.T) {
	defaultT := makeTargetWithDeps("default")
	nonDefault := makeTargetWithDeps("hidden").SetDefault(false)

	graph, err := NewBuildGraph(makeTargets("p", defaultT, nonDefault), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	visited := []string{}
	err = graph.ForEachDefault(func(n *BuildNode) error {
		visited = append(visited, n.FullName)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(visited) != 1 || visited[0] != "p:default" {
		t.Errorf("ForEachDefault visited = %v, want [p:default]", visited)
	}
}

func TestPkgBuildMetaIsRemote(t *testing.T) {
	local := PkgBuildMeta{Origin: api.SourceLocal}
	remote := PkgBuildMeta{Origin: api.SourceRemote}
	if local.IsRemote() {
		t.Error("local should not be remote")
	}
	if !remote.IsRemote() {
		t.Error("remote should be remote")
	}
}

func TestResolveDepPkgName(t *testing.T) {
	subParents := map[string]string{"parent/sub": "parent"}
	pkgMeta := map[string]PkgBuildMeta{
		"parent":     {},
		"parent/sub": {},
	}
	got := resolveDepPkgName("parent/sub", "dep", subParents, pkgMeta)
	_ = got
}

func TestBuildPath(t *testing.T) {
	got := BuildPath("/base", "abc123", "objects")
	want := filepath.Join("/base", "build", "abc123", "objects")
	if got != want {
		t.Errorf("BuildPath = %q, want %q", got, want)
	}
}
