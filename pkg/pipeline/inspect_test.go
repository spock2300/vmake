package pipeline

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestInspectMatchesBuildStageToolchainDirsAndLayout(t *testing.T) {
	mgr := toolchain.GetManager()
	host, err := mgr.GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	s := sessionFixture(t)
	name := "inspect-" + storage.OwnerKey(s.ctx.Paths.ProjectDir)[:16]
	copyTC := *host
	copyTC.Name = name
	tc := &copyTC
	if err := mgr.RegisterToolchain(name, tc); err != nil {
		t.Fatal(err)
	}
	config.SetEntry(s.ctx.Config, "//dep", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: name}})
	s.ctx.DepGraph.Roots = []string{"//app"}
	s.ctx.DepGraph.Packages["//app"].Deps = []string{"//dep"}
	s.cfg = makeBuildConfig(s.ctx, host, "host")
	s.globalFlagsHash = build.HashGlobalFlags([4][]string{})
	s.computeDirsAndOptions(t)
	if err := s.prepareAllPackages(); err != nil {
		t.Fatal(err)
	}
	inspection, err := Inspect(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"//app", "//dep"} {
		if !reflect.DeepEqual(inspection.pkgDirs[pkg], s.pkgDirs[pkg]) {
			t.Errorf("%s inspection/build directories differ: %+v / %+v", pkg, inspection.pkgDirs[pkg], s.pkgDirs[pkg])
		}
		built, inspected := s.layouts[pkg], inspection.layouts[pkg]
		if built == nil || inspected == nil {
			t.Fatalf("%s layout missing: build=%+v inspect=%+v", pkg, built, inspected)
		}
		if inspected.BuildKey != built.BuildKey || inspected.Dirs != built.Dirs ||
			inspected.Ready != built.Ready || inspected.Platform != built.Platform {
			t.Errorf("%s inspection layout differs from the build stage:\ninspect %+v\nbuild   %+v", pkg, inspected, built)
		}
	}
	if inspection.packageToolchains["//dep"] != tc || inspection.packageToolchains["//app"] != host {
		t.Fatal("inspection lost per-package toolchain selection")
	}
	missing := name + "-missing"
	called := false
	mgr.SetOnMissing(missing, func(string) (*toolchain.Toolchain, error) { called = true; return tc, nil })
	config.SetEntry(s.ctx.Config, "//dep", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: missing}})
	if _, err := Inspect(s.ctx); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("missing package toolchain was not reported: %v", err)
	}
	if called {
		t.Fatal("inspection ran a toolchain installer")
	}
}

func TestInspectWithOptionsSkipsUnresolvedPackages(t *testing.T) {
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	s := sessionFixture(t)
	s.ctx.DepGraph.Roots = []string{"//app"}
	missing := "inspect-skip-" + storage.OwnerKey(s.ctx.Paths.ProjectDir)[:16]
	config.SetEntry(s.ctx.Config, "//dep", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: missing}})

	if _, err := Inspect(s.ctx); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("strict inspection did not report the missing toolchain: %v", err)
	}
	inspection, err := InspectWithOptions(s.ctx, InspectOptions{SkipUnresolvedPackages: true})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.pkgDirs["//dep"] != nil || inspection.packageToolchains["//dep"] != nil {
		t.Fatalf("unresolved package was not skipped: %+v", inspection.pkgDirs["//dep"])
	}
	if inspection.pkgDirs["//app"] == nil || inspection.packageToolchains["//app"] != host {
		t.Fatal("skipping an unresolved package dropped resolved packages")
	}
}

func TestInspectRemoteTreeDirs(t *testing.T) {
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	s := sessionFixture(t)
	containerID, name, member := "native/root", "native/root/member", "member"
	tree := remoteTreeDir(s.ctx, containerID)
	memberDir := filepath.Join(tree, "src", member)
	if err := os.MkdirAll(memberDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memberDir, "build.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s.ctx.DepGraph.Containers[containerID] = &resolver.ContainerNode{
		ID:      containerID,
		Repo:    "native",
		Kind:    resolver.SourceKindNative,
		Dir:     filepath.Join(tree, "src"),
		URL:     "unused",
		Version: "1.0.0",
		Commit:  "owner-commit",
	}
	src := buildscript.NewSource(name, filepath.Join(memberDir, "build.go"), memberDir, api.SourceRemote)
	src.Container = containerID
	src.RelPath = member
	node := resolver.NewPackageNode(name, containerID, member, resolver.SourceKindNative, src, api.NewPackage().SetName(name))
	s.ctx.DepGraph.Packages[name] = node
	delete(s.ctx.DepGraph.Packages, "//dep")
	s.ctx.DepGraph.Order = []string{name, "//app"}
	s.ctx.DepGraph.Roots = []string{"//app"}
	s.ctx.DepGraph.Packages["//app"].Deps = []string{name}
	s.globalFlagsHash = build.HashGlobalFlags([4][]string{})

	inspection, err := Inspect(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	dirs := inspection.pkgDirs[name]
	if dirs == nil {
		t.Fatal("member directories were not inspected")
	}
	if dirs.SourceDir != memberDir {
		t.Fatalf("member SourceDir = %s, want %s", dirs.SourceDir, memberDir)
	}
	if !strings.HasPrefix(dirs.BuildDir, filepath.Join(tree, "out")+string(filepath.Separator)) {
		t.Fatalf("member BuildDir = %s, want under %s", dirs.BuildDir, filepath.Join(tree, "out"))
	}
}

func skipWithoutHostTools(t *testing.T) {
	t.Helper()
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
}

func TestInspectLocalGitWithoutMaterializedTree(t *testing.T) {
	skipWithoutHostTools(t)
	s := sessionFixture(t)
	s.ctx.DepGraph.Roots = []string{"//app"}
	s.ctx.DepGraph.Packages["//app"].Deps = []string{"//dep"}
	dep := s.ctx.DepGraph.Packages["//dep"]
	dep.Pkg.SetGit("https://example.invalid/dep.git").AddVersion("1.0.0", "v1.0.0")

	inspection, err := Inspect(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	dirs := inspection.pkgDirs["//dep"]
	if dirs == nil {
		t.Fatal("unmaterialized local git package lost its directory entry")
	}
	if dirs.BuildDir != "" {
		t.Fatalf("unmaterialized local git BuildDir = %q, want empty", dirs.BuildDir)
	}
	if dirs.SourceDir != dep.Source.Dir {
		t.Fatalf("unmaterialized local git SourceDir = %q, want %q", dirs.SourceDir, dep.Source.Dir)
	}
	if _, err := os.Stat(filepath.Join(dep.Source.Dir, "src")); !os.IsNotExist(err) {
		t.Fatalf("inspection touched the source tree: %v", err)
	}
	layout := inspection.layouts["//dep"]
	if layout == nil {
		t.Fatal("unmaterialized local git package lost its layout")
	}
	if layout.Ready {
		t.Fatalf("layout = %+v, want Ready=false until the tree exists", layout)
	}
	if layout.BuildKey != "" {
		t.Fatalf("not-ready layout invented build key %q", layout.BuildKey)
	}
	view, ok := inspection.Package("//dep")
	if !ok || view.Dirs != dirs || view.Layout != layout {
		t.Fatalf("Package(//dep) = %+v / %v, want the not-ready layout", view, ok)
	}
}

func TestInspectLocalGitWithMaterializedTree(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	skipWithoutHostTools(t)
	s := sessionFixture(t)
	s.ctx.DepGraph.Roots = []string{"//app"}
	s.ctx.DepGraph.Packages["//app"].Deps = []string{"//dep"}
	dep := s.ctx.DepGraph.Packages["//dep"]
	upstream := initGitRepo(t, map[string]string{"input.c": "original\n"})
	testGit(t, upstream, "tag", "v1.0.0")
	dep.Pkg.SetGit(upstream).AddVersion("1.0.0", "v1.0.0")
	dep.Constraints = []string{"=1.0.0"}
	if err := s.selectPackageSource("//dep"); err != nil {
		t.Fatal(err)
	}
	s.pkgDirs["//dep"] = makeLocalPkgDirs(dep.Source.Dir, localPkgKeyFromParts("cc-key", s.cfg.Mode, nil, "", s.sourceCommits["//dep"]))
	if err := s.preparePackageWorkspace("//dep"); err != nil {
		t.Fatal(err)
	}

	inspection, err := Inspect(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	dirs := inspection.pkgDirs["//dep"]
	if dirs == nil || dirs.BuildDir == "" {
		t.Fatalf("materialized local git directories = %+v, want a computed build key", dirs)
	}
	if !strings.HasPrefix(dirs.BuildDir, filepath.Join(dep.Source.Dir, "build")+string(filepath.Separator)) {
		t.Fatalf("materialized local git BuildDir = %s, want under %s", dirs.BuildDir, filepath.Join(dep.Source.Dir, "build"))
	}
}

func TestInspectFilterDepsMatchesConditionalBuildGraph(t *testing.T) {
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	s := sessionFixture(t)
	app := s.ctx.DepGraph.Packages["//app"]
	s.ctx.DepGraph.Roots = []string{"//app"}
	app.Pkg.OnRequire(func(ctx *api.RequireContext) {
		if ctx.Bool("use_dep") {
			ctx.AddRequires("//dep")
		}
	})
	options := api.NewConfigContext("//app")
	options.Option("use_dep").SetType(api.OptionBool).SetDefault(false)
	if s.ctx.allOptions == nil {
		s.ctx.allOptions = map[string]map[string]*api.Option{}
	}
	s.ctx.allOptions["//app"] = options.GetOptions()

	config.SetEntry(s.ctx.Config, "//app", &config.EntryConfig{Options: map[string]any{"use_dep": true}})
	enabled, err := InspectWithOptions(s.ctx, InspectOptions{FilterDeps: true})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.needed["//dep"] {
		t.Fatal("enabled conditional dependency was filtered out")
	}

	config.SetEntry(s.ctx.Config, "//app", &config.EntryConfig{Options: map[string]any{"use_dep": false}})
	disabled, err := InspectWithOptions(s.ctx, InspectOptions{FilterDeps: true})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.needed["//dep"] {
		t.Fatal("disabled conditional dependency remained in the inspected graph")
	}
}

func TestInspectFilterDepsAllowsNativeMemberWithoutRootPackage(t *testing.T) {
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	s := sessionFixture(t)
	containerID, memberID := "native/root", "native/root/member"
	tree := remoteTreeDir(s.ctx, containerID)
	memberDir := filepath.Join(tree, "src", "member")
	if err := os.MkdirAll(memberDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memberDir, "build.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s.ctx.DepGraph.Containers[containerID] = &resolver.ContainerNode{
		ID:      containerID,
		Repo:    "native",
		Kind:    resolver.SourceKindNative,
		Dir:     filepath.Join(tree, "src"),
		Version: "1.0.0",
		Commit:  "owner-commit",
	}
	src := buildscript.NewSource(memberID, filepath.Join(memberDir, "build.go"), memberDir, api.SourceRemote)
	src.Container = containerID
	src.RelPath = "member"
	s.ctx.DepGraph.Packages[memberID] = resolver.NewPackageNode(memberID, containerID, "member", resolver.SourceKindNative, src, api.NewPackage().SetName(memberID))
	s.ctx.DepGraph.Order = []string{memberID, "//app", "//dep"}
	s.ctx.DepGraph.Roots = []string{"//app"}
	s.ctx.DepGraph.Packages["//app"].Deps = []string{memberID}

	insp, err := InspectWithOptions(s.ctx, InspectOptions{FilterDeps: true})
	if err != nil {
		t.Fatalf("native member without root package failed inspection: %v", err)
	}
	if !insp.needed[memberID] {
		t.Fatal("native member was not marked needed")
	}
	if insp.pkgDirs[memberID] == nil || insp.pkgDirs[memberID].SourceDir != memberDir {
		t.Fatalf("member dirs = %+v, want SourceDir %s", insp.pkgDirs[memberID], memberDir)
	}
}

func TestInspectFilterDepsSkipsUnneededToolchain(t *testing.T) {
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	s := sessionFixture(t)
	s.ctx.DepGraph.Roots = []string{"//app"}
	missing := "inspect-filter-skip-" + storage.OwnerKey(s.ctx.Paths.ProjectDir)[:16]
	config.SetEntry(s.ctx.Config, "//dep", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: missing}})

	inspection, err := InspectWithOptions(s.ctx, InspectOptions{FilterDeps: true})
	if err != nil {
		t.Fatalf("filtered inspection resolved an unneeded package: %v", err)
	}
	if inspection.packageToolchains["//dep"] != nil || inspection.pkgDirs["//dep"] != nil {
		t.Fatalf("unneeded package was resolved: %+v", inspection.pkgDirs["//dep"])
	}
	if inspection.packageToolchains["//app"] != host {
		t.Fatal("needed package used the wrong toolchain")
	}
}

func TestInspectFreezeDepsPreventsRepeatedOnRequire(t *testing.T) {
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	s := sessionFixture(t)
	app := s.ctx.DepGraph.Packages["//app"]
	s.ctx.DepGraph.Roots = []string{"//app"}
	calls := 0
	app.Pkg.OnRequire(func(*api.RequireContext) { calls++ })
	for i := 0; i < 2; i++ {
		if _, err := InspectWithOptions(s.ctx, InspectOptions{FilterDeps: true, FreezeDeps: true}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("OnRequire ran %d times, want 1 after FreezeDeps", calls)
	}
}

func TestInspectionPackageView(t *testing.T) {
	dirs := &api.PkgDirs{SourceDir: "/src/app", BuildDir: "/src/app/build/key"}
	layout := &build.PackageLayout{Dirs: *dirs, BuildKey: "key", Ready: true}
	tc := &toolchain.Toolchain{Name: "host"}
	inspection := &Inspection{
		needed:            map[string]bool{"//app": true},
		pkgDirs:           map[string]*api.PkgDirs{"//app": dirs},
		layouts:           map[string]*build.PackageLayout{"//app": layout},
		packageToolchains: map[string]*toolchain.Toolchain{"//app": tc, "//skipped": tc},
		packageValues:     map[string]map[string]any{"//app": {"opt": 1}},
		packagePlatforms:  map[string]api.Platform{"//app": {OS: "none"}, "//skipped": {OS: "none"}},
		names:             []string{"//app", "//skipped"},
		nameSet:           map[string]bool{"//app": true, "//skipped": true},
	}

	view, ok := inspection.Package("//app")
	if !ok || view.Skipped {
		t.Fatal("Package(//app) reported the package as skipped")
	}
	if view.Name != "//app" || view.Dirs != dirs || view.Layout != layout || view.Toolchain != tc || !view.Needed {
		t.Fatalf("view = %+v", view)
	}
	if view.Values["opt"] != 1 || view.Platform == nil || view.Platform.OS != "none" {
		t.Fatalf("view values/platform = %+v / %+v", view.Values, view.Platform)
	}

	skipped, ok := inspection.Package("//skipped")
	if !ok || !skipped.Skipped || skipped.Dirs != nil {
		t.Fatalf("skipped view = %+v / %v, want ok=true Skipped=true", skipped, ok)
	}
	if _, ok := inspection.Package("//absent"); ok {
		t.Fatal("Package(//absent) reported ok")
	}
	if _, ok := (*Inspection)(nil).Package("//app"); ok {
		t.Fatal("Package on a nil Inspection reported ok")
	}
	if got := inspection.Packages(); !reflect.DeepEqual(got, []string{"//app", "//skipped"}) {
		t.Fatalf("Packages() = %v", got)
	}
	got := inspection.Packages()
	got[0] = "mutated"
	if inspection.Packages()[0] != "//app" {
		t.Fatal("Packages() returned a shared slice")
	}
}

func TestTargetDepsOfCanonicalizesIntraContainerRefs(t *testing.T) {
	bctx := api.NewBuildContext("", nil)
	tgt := bctx.Target("app")
	tgt.AddDeps("local_tgt", "//sub_b:utils_b", "//sub_c", "//:root_t", "other/mod/x:lib")

	rtCtx := &RuntimeContext{
		DepGraph: &resolver.Graph{
			Packages: map[string]*resolver.PackageNode{
				"myorg/sdk/sub_a": {ID: "myorg/sdk/sub_a", Container: "myorg/sdk"},
				"//app":           {ID: "//app", Container: "//"},
			},
		},
	}
	got := targetDepsOf(rtCtx, "myorg/sdk/sub_a", tgt, rtCtx.DepGraph.Packages["myorg/sdk/sub_a"])
	want := []string{
		"local_tgt",
		"myorg/sdk/sub_b:utils_b",
		"myorg/sdk/sub_c",
		"myorg/sdk:root_t",
		"other/mod/x:lib",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targetDepsOf() = %v, want %v", got, want)
	}

	gotLocal := targetDepsOf(rtCtx, "//app", tgt, rtCtx.DepGraph.Packages["//app"])
	wantLocal := []string{
		"local_tgt",
		"//sub_b:utils_b",
		"//sub_c",
		"//:root_t",
		"other/mod/x:lib",
	}
	if !reflect.DeepEqual(gotLocal, wantLocal) {
		t.Fatalf("targetDepsOf(local) = %v, want %v", gotLocal, wantLocal)
	}
}
