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

	graph, err := NewBuildGraph(makeTargets("//p", app, lib), nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}

	appNode, err := graph.GetNode("//p:app")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(appNode.Deps, []string{"//p:lib"}) {
		t.Errorf("app deps = %v, want [//p:lib]", appNode.Deps)
	}
}

func TestBuildGraphCrossPackageDepExplicit(t *testing.T) {
	lib := makeTargetWithDeps("lib")
	app := makeTargetWithDeps("app")
	app.AddDeps("//other:lib")

	targets := map[string]map[string]*api.Target{
		"//main":  {"app": app},
		"//other": {"lib": lib},
	}
	graph, err := NewBuildGraph(targets, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	appNode, _ := graph.GetNode("//main:app")
	if !reflect.DeepEqual(appNode.Deps, []string{"//other:lib"}) {
		t.Errorf("cross-package dep = %v", appNode.Deps)
	}
}

func TestBuildGraphPackageRefExpandsAllTargets(t *testing.T) {
	lib1 := makeTargetWithDeps("lib1")
	lib2 := makeTargetWithDeps("lib2")
	app := makeTargetWithDeps("app")
	app.AddDeps("//other:*")

	targets := map[string]map[string]*api.Target{
		"//main":  {"app": app},
		"//other": {"lib1": lib1, "lib2": lib2},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"//main":  {Container: "//", Deps: []string{"//other"}},
		"//other": {Container: "//"},
	}
	graph, err := NewBuildGraph(targets, pkgMeta)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	appNode, _ := graph.GetNode("//main:app")
	got := append([]string{}, appNode.Deps...)
	sort.Strings(got)
	want := []string{"//other:lib1", "//other:lib2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("package-ref expansion = %v, want %v", got, want)
	}
}

func TestBuildGraphWildcardDep(t *testing.T) {
	lib1 := makeTargetWithDeps("lib1")
	lib2 := makeTargetWithDeps("lib2")
	app := makeTargetWithDeps("app")
	app.AddDeps("//other:*")

	targets := map[string]map[string]*api.Target{
		"//main":  {"app": app},
		"//other": {"lib1": lib1, "lib2": lib2},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"//main":  {Container: "//", Deps: []string{"//other"}},
		"//other": {Container: "//"},
	}
	graph, err := NewBuildGraph(targets, pkgMeta)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	appNode, _ := graph.GetNode("//main:app")
	got := append([]string{}, appNode.Deps...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"//other:lib1", "//other:lib2"}) {
		t.Errorf("wildcard dep = %v, want [//other:lib1 //other:lib2]", got)
	}
}

func TestBuildGraphMissingDep(t *testing.T) {
	app := makeTargetWithDeps("app")
	app.AddDeps("nonexistent")

	_, err := NewBuildGraph(makeTargets("//p", app), nil)
	if err == nil {
		t.Error("missing dep should produce error")
	}
}

func TestBuildGraphTopologicalOrder(t *testing.T) {
	lib := makeTargetWithDeps("lib")
	app := makeTargetWithDeps("app", "lib")

	graph, err := NewBuildGraph(makeTargets("//p", app, lib), nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	pos := make(map[string]int)
	for i, n := range graph.Order {
		pos[n] = i
	}
	if pos["//p:lib"] >= pos["//p:app"] {
		t.Errorf("lib should come before app: lib=%d app=%d", pos["//p:lib"], pos["//p:app"])
	}
}

func TestBuildGraphCycleError(t *testing.T) {
	a := makeTargetWithDeps("a", "b")
	b := makeTargetWithDeps("b", "a")
	_, err := NewBuildGraph(makeTargets("//p", a, b), nil)
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

	graph, err := NewBuildGraph(makeTargets("//p", defaultT, nonDefault), nil)
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
	if len(visited) != 1 || visited[0] != "//p:default" {
		t.Errorf("ForEachDefault visited = %v, want [//p:default]", visited)
	}
}

func TestBuildGraphForEachDefaultSkipsTestsUnlessIncluded(t *testing.T) {
	defaultT := makeTargetWithDeps("default")
	testT := makeTargetWithDeps("tester").SetTest(true)

	graph, err := NewBuildGraph(makeTargets("//p", defaultT, testT), nil)
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
	if len(visited) != 1 || visited[0] != "//p:default" {
		t.Errorf("ForEachDefault(false) visited = %v, want [//p:default]", visited)
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

func TestResolveDepPkgNameCanonicalizesByContainer(t *testing.T) {
	pkgMeta := map[string]PkgBuildMeta{
		"repo/parent/sub": {Container: "repo/parent"},
		"//app":           {Container: "//"},
	}
	d := newDepResolver(nil, pkgMeta)
	if got := d.resolveDepPkgName("repo/parent/sub", "//dep"); got != "repo/parent/dep" {
		t.Errorf("resolveDepPkgName = %q, want repo/parent/dep", got)
	}
	if got := d.resolveDepPkgName("repo/parent/sub", "//"); got != "repo/parent" {
		t.Errorf("resolveDepPkgName(//) = %q, want repo/parent", got)
	}
	if got := d.resolveDepPkgName("//app", "//lib"); got != "//lib" {
		t.Errorf("resolveDepPkgName local = %q, want //lib", got)
	}
}

func TestBuildGraphResolvesIntraContainerTargetDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "//lib:build")
	lib := makeTargetWithDeps("build")
	targets := map[string]map[string]*api.Target{
		"repo/parent/tool": {"app": app},
		"repo/parent/lib":  {"build": lib},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"repo/parent/tool": {Container: "repo/parent"},
		"repo/parent/lib":  {Container: "repo/parent"},
	}

	graph, err := NewBuildGraph(targets, pkgMeta)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	node, _ := graph.GetNode("repo/parent/tool:app")
	if !reflect.DeepEqual(node.Deps, []string{"repo/parent/lib:build"}) {
		t.Fatalf("deps = %v, want [repo/parent/lib:build]", node.Deps)
	}
}

func TestBuildGraphResolvesIntraContainerWildcardDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "//lib:*")
	lib := makeTargetWithDeps("build")
	extra := makeTargetWithDeps("extra")
	targets := map[string]map[string]*api.Target{
		"repo/parent/tool": {"app": app},
		"repo/parent/lib":  {"build": lib, "extra": extra},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"repo/parent/tool": {Container: "repo/parent"},
		"repo/parent/lib":  {Container: "repo/parent"},
	}

	graph, err := NewBuildGraph(targets, pkgMeta)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	node, _ := graph.GetNode("repo/parent/tool:app")
	got := append([]string{}, node.Deps...)
	sort.Strings(got)
	want := []string{"repo/parent/lib:build", "repo/parent/lib:extra"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deps = %v, want %v", got, want)
	}
}

func TestBuildGraphBareNameStaysOwnTarget(t *testing.T) {
	app := makeTargetWithDeps("app", "build")
	own := makeTargetWithDeps("build")
	other := makeTargetWithDeps("build")
	targets := map[string]map[string]*api.Target{
		"repo/parent/tool": {"app": app, "build": own},
		"//build":          {"build": other},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"repo/parent/tool": {Container: "repo/parent"},
		"//build":          {Container: "//"},
	}

	graph, err := NewBuildGraph(targets, pkgMeta)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	node, _ := graph.GetNode("repo/parent/tool:app")
	if !reflect.DeepEqual(node.Deps, []string{"repo/parent/tool:build"}) {
		t.Fatalf("deps = %v, want own target repo/parent/tool:build", node.Deps)
	}
}

func TestBuildGraphMissingIntraContainerDepShowsResolved(t *testing.T) {
	app := makeTargetWithDeps("app", "//ghost:build")
	targets := map[string]map[string]*api.Target{"repo/parent/tool": {"app": app}}
	pkgMeta := map[string]PkgBuildMeta{"repo/parent/tool": {Container: "repo/parent"}}

	_, err := NewBuildGraph(targets, pkgMeta)
	if err == nil {
		t.Fatal("missing dependency should fail")
	}
	if !strings.Contains(err.Error(), "repo/parent/ghost:build") {
		t.Errorf("error should mention resolved repo/parent/ghost:build, got: %v", err)
	}
}

func TestBuildGraphDiamondPackageRefMemo(t *testing.T) {
	app := makeTargetWithDeps("app", "//left:*", "//right:*")
	leftLib := makeTargetWithDeps("lib")
	rightLib := makeTargetWithDeps("lib")
	commonLib := makeTargetWithDeps("common")

	targets := map[string]map[string]*api.Target{
		"//main":   {"app": app},
		"//left":   {"lib": leftLib},
		"//right":  {"lib": rightLib},
		"//common": {"common": commonLib},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"//main":   {Container: "//", Deps: []string{"//left", "//right"}},
		"//left":   {Container: "//", Deps: []string{"//common"}},
		"//right":  {Container: "//", Deps: []string{"//common"}},
		"//common": {Container: "//"},
	}
	graph, err := NewBuildGraph(targets, pkgMeta)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	appNode, _ := graph.GetNode("//main:app")
	got := append([]string{}, appNode.Deps...)
	sort.Strings(got)
	want := []string{"//common:common", "//left:lib", "//right:lib"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diamond deps = %v, want %v", got, want)
	}
}

func TestBuildGraphDiamondCycleError(t *testing.T) {
	app := makeTargetWithDeps("app", "//left:*")
	leftLib := makeTargetWithDeps("lib")
	commonLib := makeTargetWithDeps("common")

	targets := map[string]map[string]*api.Target{
		"//main":   {"app": app},
		"//left":   {"lib": leftLib},
		"//common": {"common": commonLib},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"//main":   {Container: "//", Deps: []string{"//left"}},
		"//left":   {Container: "//", Deps: []string{"//common"}},
		"//common": {Container: "//", Deps: []string{"//left"}},
	}
	if _, err := NewBuildGraph(targets, pkgMeta); err == nil {
		t.Error("diamond with cycle should produce error")
	}
}

func TestBuildPath(t *testing.T) {
	if got, want := BuildPath("/base", "abc123", "objects"), filepath.Join("/base", "build", "abc123", "objects"); got != want {
		t.Errorf("BuildPath = %q, want %q", got, want)
	}
	full := "761fa64b39dfa1018158230bb18f2784f5503d08d8b81918e33fb2614d1fb386"
	got := BuildPath("/base", full, "staging/include")
	want := filepath.Join("/base", "build", "761fa64b39dfa101", "staging/include")
	if got != want {
		t.Errorf("BuildPath with a full key = %q, want %q", got, want)
	}
}

func TestBuildGraphUseRequiresAndPackagePathDep(t *testing.T) {
	chip := makeTargetWithDeps("chip")
	board := makeTargetWithDeps("board", "//chip")
	app := makeTargetWithDeps("app").UseRequires()

	targets := map[string]map[string]*api.Target{
		"//app":   {"app": app},
		"//board": {"board": board},
		"//chip":  {"chip": chip},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"//app":   {Container: "//", Deps: []string{"//board"}},
		"//board": {Container: "//", Deps: []string{"//chip"}},
		"//chip":  {Container: "//"},
	}
	graph, err := NewBuildGraph(targets, pkgMeta)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	appNode, _ := graph.GetNode("//app:app")
	got := append([]string{}, appNode.Deps...)
	sort.Strings(got)
	want := []string{"//board:board", "//chip:chip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("app deps = %v, want %v", got, want)
	}
}

func TestBuildGraphCrossPackageReverseTargetCycleRejected(t *testing.T) {
	t1 := makeTargetWithDeps("t1", "//pkgB:t2")
	t4 := makeTargetWithDeps("t4")
	t2 := makeTargetWithDeps("t2")
	t3 := makeTargetWithDeps("t3", "//pkgA:t4")
	top := makeTargetWithDeps("top", "//pkgA:t1")

	targets := map[string]map[string]*api.Target{
		"//root": {"top": top},
		"//pkgA": {"t1": t1, "t4": t4},
		"//pkgB": {"t2": t2, "t3": t3},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"//root": {Container: "//"},
		"//pkgA": {Container: "//"},
		"//pkgB": {Container: "//"},
	}

	_, err := NewBuildGraph(targets, pkgMeta)
	if err == nil {
		t.Fatal("expected cross-package reverse target cycle error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "package circular dependency detected: //pkgA -> //pkgB -> //pkgA") {
		t.Fatalf("unexpected cycle message: %s", msg)
	}
	if strings.Contains(msg, "//root") {
		t.Fatalf("upstream bystander root should be excluded from cycle path: %s", msg)
	}
	if !strings.Contains(msg, "//pkgA:t1 -> //pkgB:t2") || !strings.Contains(msg, "//pkgB:t3 -> //pkgA:t4") {
		t.Fatalf("expected triggering target edges in message: %s", msg)
	}
}

func TestBuildGraphSamePackageTargetCycleExactChain(t *testing.T) {
	mainT := makeTargetWithDeps("main", "a")
	a := makeTargetWithDeps("a", "b")
	b := makeTargetWithDeps("b", "a")

	_, err := NewBuildGraph(makeTargets("//p", mainT, a, b), nil)
	if err == nil {
		t.Fatal("expected target cycle error")
	}
	want := "circular dependency detected: //p:a -> //p:b -> //p:a"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

func TestBuildGraphInternalVisibilityEnforced(t *testing.T) {
	wire := makeTargetWithDeps("wire")
	netLib := makeTargetWithDeps("net", "//net/internal/wire:wire")
	app := makeTargetWithDeps("app", "//net/internal/wire:wire")

	meta := map[string]PkgBuildMeta{
		"//net/internal/wire": {Container: "//", VisPath: "@local/net/internal/wire"},
		"//net":               {Container: "//", VisPath: "@local/net"},
		"//app":               {Container: "//", VisPath: "@local/app"},
	}

	okTargets := map[string]map[string]*api.Target{
		"//net/internal/wire": {"wire": wire},
		"//net":               {"net": netLib},
	}
	if _, err := NewBuildGraph(okTargets, map[string]PkgBuildMeta{
		"//net/internal/wire": meta["//net/internal/wire"],
		"//net":               meta["//net"],
	}); err != nil {
		t.Fatalf("expected //net -> //net/internal/wire allowed, got %v", err)
	}

	badTargets := map[string]map[string]*api.Target{
		"//net/internal/wire": {"wire": wire},
		"//app":               {"app": app},
	}
	_, err := NewBuildGraph(badTargets, map[string]PkgBuildMeta{
		"//net/internal/wire": meta["//net/internal/wire"],
		"//app":               meta["//app"],
	})
	if err == nil || !strings.Contains(err.Error(), `use of internal package "//net/internal/wire" not allowed from package "//app"`) {
		t.Fatalf("expected internal visibility error, got %v", err)
	}
}

func TestBuildGraphZeroTargetPublicUmbrellaWrappingInternalPackage(t *testing.T) {
	secretCore := makeTargetWithDeps("core")
	app := makeTargetWithDeps("app", "//sdk").UseRequires()

	targets := map[string]map[string]*api.Target{
		"//sdk/internal/secret": {"core": secretCore},
		"//app":                 {"app": app},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"//sdk":                 {Container: "//", Deps: []string{"//sdk/internal/secret"}, VisPath: "@local/sdk"},
		"//sdk/internal/secret": {Container: "//", VisPath: "@local/sdk/internal/secret"},
		"//app":                 {Container: "//", Deps: []string{"//sdk"}, VisPath: "@local/app"},
	}

	graph, err := NewBuildGraph(targets, pkgMeta)
	if err != nil {
		t.Fatalf("0-target public umbrella wrapping internal package must be allowed, got: %v", err)
	}
	appNode, err := graph.GetNode("//app:app")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(appNode.Deps, []string{"//sdk/internal/secret:core"}) {
		t.Fatalf("//app:app Deps = %v, want [//sdk/internal/secret:core]", appNode.Deps)
	}

	_, err = NewBuildGraph(map[string]map[string]*api.Target{"//a": {"t": makeTargetWithDeps("t")}}, map[string]PkgBuildMeta{
		"//a": {Container: "//", SubGraphs: []string{"//a"}, VisPath: "@local/a"},
	})
	if err == nil || !strings.Contains(err.Error(), "package circular dependency detected: //a -> //a") {
		t.Fatalf("expected self BuildSubGraph cycle error, got %v", err)
	}
}

func TestDependencyResolversValidateTargetExistenceUniformly(t *testing.T) {
	nodes := map[string]*BuildNode{
		"//p:app": {FullName: "//p:app", PkgName: "//p", Target: makeTargetWithDeps("app")},
	}
	d := newDepResolver(nodes, map[string]PkgBuildMeta{"//p": {Container: "//"}, "//q": {Container: "//"}})
	if _, err := d.resolveDep("//q:ghost", "//p", []string{"//p:app"}); err == nil {
		t.Fatal("resolveDep accepted an unknown target")
	}
	if _, err := d.resolveDirectDep("//q:ghost", "//p", []string{"//p:app"}); err == nil {
		t.Fatal("resolveDirectDep accepted an unknown target")
	}
}

func TestAuditBuildGraphReportsMissingDependencyForUndeclaredOnBuildCrossPkg(t *testing.T) {
	app := makeTargetWithDeps("app", "hl/sdk/internal/core:lib")
	lib := makeTargetWithDeps("lib")
	g := AuditBuildGraph(
		map[string]map[string]*api.Target{
			"//app":                {"app": app},
			"hl/sdk/internal/core": {"lib": lib},
		},
		map[string]PkgBuildMeta{
			"//app":                {Container: "//", VisPath: "@local/app"},
			"hl/sdk/internal/core": {Container: "hl/sdk", VisPath: "hl/sdk/internal/core"},
		},
		GraphAuditOptions{
			Needed:   map[string]bool{"//app": true},
			Included: map[string]bool{"//app": true},
		},
	)
	var internalCount, missingCount int
	for _, issue := range g.Issues {
		switch issue.Kind {
		case IssueKindInternalVisibility:
			internalCount++
		case IssueKindMissingDependency:
			missingCount++
		}
	}
	if internalCount != 1 || missingCount != 1 {
		t.Fatalf("issues = %+v, want 1 internal-visibility and 1 missing-dependency", g.Issues)
	}
}
