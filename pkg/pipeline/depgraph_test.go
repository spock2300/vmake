package pipeline

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestResolvePackageRefCanonicalizesContainerRelative(t *testing.T) {
	ctx := &RuntimeContext{
		Resolver: resolver.NewResolver(nil, t.TempDir()),
	}
	ctx.DepGraph = ctx.Resolver.Graph()
	ctx.DepGraph.Packages["//from"] = &resolver.PackageNode{ID: "//from", Container: "//"}
	ctx.DepGraph.Packages["hl/sdk/sub_a"] = &resolver.PackageNode{ID: "hl/sdk/sub_a", Container: "hl/sdk"}

	for _, tc := range []struct {
		name string
		from string
		ref  string
		want string
	}{
		{"empty", "//from", "", ""},
		{"external path", "//from", "hl/sdk/sub_a", "hl/sdk/sub_a"},
		{"local ref from local", "//from", "//dep", "//dep"},
		{"container-relative ref from native", "hl/sdk/sub_a", "//sub_b", "hl/sdk/sub_b"},
		{"container root ref from native", "hl/sdk/sub_a", "//", "hl/sdk"},
	} {
		if got := ResolvePackageRef(ctx, NeededSet{tc.from: true}, tc.from, tc.ref); got != tc.want {
			t.Fatalf("%s: ResolvePackageRef = %q, want %q", tc.name, got, tc.want)
		}
	}

	if got := ResolvePackageRef(nil, nil, "//from", "//dep"); got != "//dep" {
		t.Fatalf("nil context = %q, want //dep", got)
	}
}

func TestBuildUnifiedGraphDeclaresTargetsOnce(t *testing.T) {
	s := sessionFixture(t)
	declarations := 0
	app := s.ctx.DepGraph.Packages["//app"]
	app.Pkg.OnBuild(func(ctx *api.BuildContext) {
		declarations++
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
	inspection := &Inspection{
		needed:  NeededSet{"//app": true},
		pkgDirs: map[string]*api.PkgDirs{"//app": {SourceDir: app.Source.Dir}},
	}

	g, err := BuildUnifiedGraph(s.ctx, inspection, GraphOptions{IncludeAll: true, DeclareTargets: true})
	if err != nil {
		t.Fatal(err)
	}
	if declarations != 1 {
		t.Fatalf("OnBuild ran %d times, want 1", declarations)
	}
	targets := g.DeclaredTargets("//app")
	if len(targets) != 1 || targets["app"] == nil {
		t.Fatalf("DeclaredTargets(//app) = %+v", targets)
	}
	buildCtx, ok := g.BuildContext("//app")
	if !ok || buildCtx == nil {
		t.Fatal("BuildContext(//app) missing after a declaration")
	}
	if again := g.DeclaredTargets("//app"); len(again) != len(targets) {
		t.Fatalf("re-reading declarations changed them: %+v", again)
	}

	quiet, err := BuildUnifiedGraph(s.ctx, inspection, GraphOptions{IncludeAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if declarations != 1 {
		t.Fatalf("OnBuild ran %d times with DeclareTargets=false, want 1", declarations)
	}
	if got := quiet.DeclaredTargets("//app"); len(got) != 0 {
		t.Fatalf("DeclareTargets=false still declared %+v", got)
	}
	if _, ok := quiet.BuildContext("//app"); ok {
		t.Fatal("DeclareTargets=false still stored a build context")
	}
}

func TestWhyTargetReportsDeclarationFailureForUnneededPackage(t *testing.T) {
	s := sessionFixture(t)
	app := s.ctx.DepGraph.Packages["//app"]
	app.Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
	s.ctx.DepGraph.Packages["//dep"].Pkg.OnBuild(func(ctx *api.BuildContext) {
		panic("dep buildscript exploded")
	})
	inspection := &Inspection{
		needed:  NeededSet{"//app": true},
		pkgDirs: map[string]*api.PkgDirs{"//app": {SourceDir: app.Source.Dir}},
	}

	g, err := BuildUnifiedGraph(s.ctx, inspection, GraphOptions{DeclareTargets: true})
	if err != nil {
		t.Fatal(err)
	}
	res := WhyTarget(s.ctx, g, "//dep:dep", false)
	if res.Exists {
		t.Fatalf("WhyTarget(Exists) = true, reason %q", res.Reason)
	}
	if !strings.Contains(res.Reason, "cannot declare targets for //dep") {
		t.Fatalf("reason = %q, want the declaration failure", res.Reason)
	}
	if !strings.Contains(res.Reason, "exploded") {
		t.Fatalf("reason = %q, want the buildscript error", res.Reason)
	}
	declared := false
	for _, issue := range g.Issues {
		if issue.Kind == "target-declare" && issue.Package == "//dep" && issue.Target == "//dep:dep" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("issues = %+v, want a target-declare issue for //dep", g.Issues)
	}
}
