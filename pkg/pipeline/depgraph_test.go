package pipeline

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestResolvePackageRefPassthrough(t *testing.T) {
	ctx := &RuntimeContext{
		Resolver: resolver.NewResolver(nil, t.TempDir()),
	}
	ctx.DepGraph = ctx.Resolver.Graph()

	for _, tc := range []struct {
		name string
		from string
		ref  string
		want string
	}{
		{"empty", "from", "", ""},
		{"package path", "from", "hl/sdk", "hl/sdk"},
		{"unknown bare name", "from", "dep", "dep"},
	} {
		if got := ResolvePackageRef(ctx, NeededSet{"from": true}, tc.from, tc.ref); got != tc.want {
			t.Fatalf("%s: ResolvePackageRef = %q, want %q", tc.name, got, tc.want)
		}
	}

	if got := ResolvePackageRef(nil, nil, "from", "dep"); got != "dep" {
		t.Fatalf("nil context = %q, want the reference unchanged", got)
	}
}

func TestResolvePackageRefFindsKnownPackage(t *testing.T) {
	ctx := &RuntimeContext{
		Resolver: resolver.NewResolver(nil, t.TempDir()),
	}
	ctx.DepGraph = ctx.Resolver.Graph()
	ctx.DepGraph.Packages["from"] = neededNode("from")
	ctx.DepGraph.Packages["dep"] = neededNode("dep")

	if got := ResolvePackageRef(ctx, NeededSet{"from": true, "dep": true}, "from", "dep"); got != "dep" {
		t.Fatalf("ResolvePackageRef = %q, want dep", got)
	}
	if got := ResolvePackageRef(ctx, NeededSet{"from": true}, "from", "dep"); got != "dep" {
		t.Fatalf("ResolvePackageRef with unneeded target = %q, want dep", got)
	}
}

// BuildUnifiedGraph must declare a package's targets at most once, and the
// declaration context it keeps must carry the data target snapshots do not
// (install items, sub-graph registrations).
func TestBuildUnifiedGraphDeclaresTargetsOnce(t *testing.T) {
	s := sessionFixture(t)
	declarations := 0
	app := s.ctx.DepGraph.Packages["app"]
	app.Pkg.SetRoot(true).OnBuild(func(ctx *api.BuildContext) {
		declarations++
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
	inspection := &Inspection{
		needed:  NeededSet{"app": true},
		pkgDirs: map[string]*api.PkgDirs{"app": {SourceDir: app.Source.Dir}},
	}

	g, err := BuildUnifiedGraph(s.ctx, inspection, GraphOptions{IncludeAll: true, DeclareTargets: true})
	if err != nil {
		t.Fatal(err)
	}
	if declarations != 1 {
		t.Fatalf("OnBuild ran %d times, want 1", declarations)
	}
	targets := g.DeclaredTargets("app")
	if len(targets) != 1 || targets["app"] == nil {
		t.Fatalf("DeclaredTargets(app) = %+v", targets)
	}
	buildCtx, ok := g.BuildContext("app")
	if !ok || buildCtx == nil {
		t.Fatal("BuildContext(app) missing after a declaration")
	}
	if again := g.DeclaredTargets("app"); len(again) != len(targets) {
		t.Fatalf("re-reading declarations changed them: %+v", again)
	}

	quiet, err := BuildUnifiedGraph(s.ctx, inspection, GraphOptions{IncludeAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if declarations != 1 {
		t.Fatalf("OnBuild ran %d times with DeclareTargets=false, want 1", declarations)
	}
	if got := quiet.DeclaredTargets("app"); len(got) != 0 {
		t.Fatalf("DeclareTargets=false still declared %+v", got)
	}
	if _, ok := quiet.BuildContext("app"); ok {
		t.Fatal("DeclareTargets=false still stored a build context")
	}
}

// deps why can be asked about a package the active roots do not need. The probe
// must declare it with real wiring and report a declaration failure as such,
// instead of answering "target not found".
func TestWhyTargetReportsDeclarationFailureForUnneededPackage(t *testing.T) {
	s := sessionFixture(t)
	app := s.ctx.DepGraph.Packages["app"]
	app.Pkg.SetRoot(true).OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
	s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(ctx *api.BuildContext) {
		panic("dep buildscript exploded")
	})
	inspection := &Inspection{
		needed:  NeededSet{"app": true},
		pkgDirs: map[string]*api.PkgDirs{"app": {SourceDir: app.Source.Dir}},
	}

	g, err := BuildUnifiedGraph(s.ctx, inspection, GraphOptions{DeclareTargets: true})
	if err != nil {
		t.Fatal(err)
	}
	res := WhyTarget(s.ctx, g, "dep:dep", false)
	if res.Exists {
		t.Fatalf("WhyTarget(Exists) = true, reason %q", res.Reason)
	}
	if !strings.Contains(res.Reason, "cannot declare targets for dep") {
		t.Fatalf("reason = %q, want the declaration failure", res.Reason)
	}
	if !strings.Contains(res.Reason, "exploded") {
		t.Fatalf("reason = %q, want the buildscript error", res.Reason)
	}
	declared := false
	for _, issue := range g.Issues {
		if issue.Kind == "target-declare" && issue.Package == "dep" && issue.Target == "dep:dep" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("issues = %+v, want a target-declare issue for dep", g.Issues)
	}
}
