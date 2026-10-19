package build

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func makeTargetWithDeps(name string, deps ...string) *api.Target {
	tr := api.NewTargetRegistry()
	t := tr.Target(name)
	t.AddDeps(deps)
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
	err = graph.ForEachDefault(false, func(n *BuildNode) error {
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

func TestBuildGraphForEachDefaultSkipsTestsUnlessIncluded(t *testing.T) {
	defaultT := makeTargetWithDeps("default")
	testT := makeTargetWithDeps("tester").SetTest(true)

	graph, err := NewBuildGraph(makeTargets("p", defaultT, testT), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	visited := []string{}
	if err := graph.ForEachDefault(false, func(n *BuildNode) error {
		visited = append(visited, n.FullName)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(visited) != 1 || visited[0] != "p:default" {
		t.Errorf("ForEachDefault(false) visited = %v, want [p:default]", visited)
	}

	visited = []string{}
	if err := graph.ForEachDefault(true, func(n *BuildNode) error {
		visited = append(visited, n.FullName)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(visited) != 2 {
		t.Errorf("ForEachDefault(true) visited = %v, want 2 entries", visited)
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
		"parent/dep": {},
	}
	d := newDepResolver(nil, pkgMeta, subParents)
	if got := d.resolveDepPkgName("parent/sub", "dep"); got != "parent/dep" {
		t.Errorf("resolveDepPkgName = %q, want parent/dep", got)
	}
	if got := d.resolveDepPkgName("parent/sub", "nomatch"); got != "nomatch" {
		t.Errorf("unresolvable dep should stay bare, got %q", got)
	}
}

func TestBuildGraphResolvesSubPackageTargetDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "lib:build")
	lib := makeTargetWithDeps("build")
	targets := map[string]map[string]*api.Target{
		"parent/tool": {"app": app},
		"parent/lib":  {"build": lib},
	}
	pkgMeta := map[string]PkgBuildMeta{"parent/tool": {}, "parent/lib": {}}
	subParents := map[string]string{"parent/tool": "parent", "parent/lib": "parent"}

	graph, err := NewBuildGraph(targets, pkgMeta, subParents)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	node, _ := graph.GetNode("parent/tool:app")
	if !reflect.DeepEqual(node.Deps, []string{"parent/lib:build"}) {
		t.Fatalf("deps = %v, want [parent/lib:build]", node.Deps)
	}
}

func TestBuildGraphResolvesSubPackageWildcardDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "lib:*")
	lib := makeTargetWithDeps("build")
	extra := makeTargetWithDeps("extra")
	targets := map[string]map[string]*api.Target{
		"parent/tool": {"app": app},
		"parent/lib":  {"build": lib, "extra": extra},
	}
	pkgMeta := map[string]PkgBuildMeta{"parent/tool": {}, "parent/lib": {}}
	subParents := map[string]string{"parent/tool": "parent", "parent/lib": "parent"}

	graph, err := NewBuildGraph(targets, pkgMeta, subParents)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	node, _ := graph.GetNode("parent/tool:app")
	got := append([]string{}, node.Deps...)
	sort.Strings(got)
	want := []string{"parent/lib:build", "parent/lib:extra"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deps = %v, want %v", got, want)
	}
}

func TestBuildGraphPrefersNearestSubPackageDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "lib:build")
	near := makeTargetWithDeps("build")
	far := makeTargetWithDeps("build")
	targets := map[string]map[string]*api.Target{
		"parent/tool": {"app": app},
		"parent/lib":  {"build": near},
		"lib":         {"build": far},
	}
	pkgMeta := map[string]PkgBuildMeta{"parent/tool": {}, "parent/lib": {}, "lib": {}}
	subParents := map[string]string{"parent/tool": "parent", "parent/lib": "parent"}

	graph, err := NewBuildGraph(targets, pkgMeta, subParents)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	node, _ := graph.GetNode("parent/tool:app")
	if !reflect.DeepEqual(node.Deps, []string{"parent/lib:build"}) {
		t.Fatalf("deps = %v, want nearest parent/lib:build", node.Deps)
	}
}

func TestBuildGraphBareNameStaysOwnTarget(t *testing.T) {
	app := makeTargetWithDeps("app", "build")
	own := makeTargetWithDeps("build")
	other := makeTargetWithDeps("build")
	targets := map[string]map[string]*api.Target{
		"parent/tool": {"app": app, "build": own},
		"build":       {"build": other},
	}
	pkgMeta := map[string]PkgBuildMeta{"parent/tool": {}, "build": {}}
	subParents := map[string]string{"parent/tool": "parent"}

	graph, err := NewBuildGraph(targets, pkgMeta, subParents)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	node, _ := graph.GetNode("parent/tool:app")
	if !reflect.DeepEqual(node.Deps, []string{"parent/tool:build"}) {
		t.Fatalf("deps = %v, want own target parent/tool:build", node.Deps)
	}
}

func TestBuildGraphMissingSubPackageDepListsCandidates(t *testing.T) {
	app := makeTargetWithDeps("app", "ghost:build")
	targets := map[string]map[string]*api.Target{"parent/tool": {"app": app}}
	pkgMeta := map[string]PkgBuildMeta{"parent/tool": {}}
	subParents := map[string]string{"parent/tool": "parent"}

	_, err := NewBuildGraph(targets, pkgMeta, subParents)
	if err == nil {
		t.Fatal("missing short dependency should fail")
	}
	for _, want := range []string{"tried sub-package candidates", "parent/tool/ghost", "parent/ghost"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestBuildGraphDiamondPackageRefMemo(t *testing.T) {
	app := makeTargetWithDeps("app", "left:*", "right:*")
	leftLib := makeTargetWithDeps("lib")
	rightLib := makeTargetWithDeps("lib")
	commonLib := makeTargetWithDeps("common")

	targets := map[string]map[string]*api.Target{
		"main":   {"app": app},
		"left":   {"lib": leftLib},
		"right":  {"lib": rightLib},
		"common": {"common": commonLib},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"main":   {Deps: []string{"left", "right"}},
		"left":   {Deps: []string{"common"}},
		"right":  {Deps: []string{"common"}},
		"common": {},
	}
	graph, err := NewBuildGraph(targets, pkgMeta, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	appNode, _ := graph.GetNode("main:app")
	got := append([]string{}, appNode.Deps...)
	sort.Strings(got)
	want := []string{"common:common", "left:lib", "right:lib"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diamond deps = %v, want %v", got, want)
	}
}

func TestBuildGraphDiamondCycleError(t *testing.T) {
	app := makeTargetWithDeps("app", "left:*")
	leftLib := makeTargetWithDeps("lib")
	commonLib := makeTargetWithDeps("common")

	targets := map[string]map[string]*api.Target{
		"main":   {"app": app},
		"left":   {"lib": leftLib},
		"common": {"common": commonLib},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"main":   {Deps: []string{"left"}},
		"left":   {Deps: []string{"common"}},
		"common": {Deps: []string{"left"}},
	}
	if _, err := NewBuildGraph(targets, pkgMeta, nil); err == nil {
		t.Error("diamond with cycle should produce error")
	}
}

func TestBuildPath(t *testing.T) {
	got := BuildPath("/base", "abc123", "objects")
	want := filepath.Join("/base", "build", "abc123", "objects")
	if got != want {
		t.Errorf("BuildPath = %q, want %q", got, want)
	}
}

func TestBuildGraphUseRequiresAndBarePackageDep(t *testing.T) {
	chip := makeTargetWithDeps("chip")
	board := makeTargetWithDeps("board", "chip")
	app := makeTargetWithDeps("app").UseRequires()

	targets := map[string]map[string]*api.Target{
		"app":   {"app": app},
		"board": {"board": board},
		"chip":  {"chip": chip},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"app":   {Deps: []string{"board"}},
		"board": {Deps: []string{"chip"}},
		"chip":  {},
	}
	graph, err := NewBuildGraph(targets, pkgMeta, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	appNode, _ := graph.GetNode("app:app")
	got := append([]string{}, appNode.Deps...)
	sort.Strings(got)
	want := []string{"board:board", "chip:chip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("app deps = %v, want %v", got, want)
	}
}
