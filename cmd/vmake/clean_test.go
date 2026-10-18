package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestCleanPackagesRemovesOnlyActiveBuildDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "build.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	pkg := api.NewPackage().SetName("app").SetRoot(true)
	r := resolver.NewResolver(nil, filepath.Join(root, ".vmake_deps"))
	r.Graph().Packages["app"] = resolver.NewPackageNode("app", buildscript.NewSource("app", path, root, api.SourceLocal), pkg)
	r.Graph().Order = []string{"app"}
	ctx := &RuntimeContext{
		Resolver: r, DepGraph: r.Graph(),
		Config: &config.ConfigFile{Global: &config.GlobalConfig{Toolchain: "host"}},
		Paths:  &pipeline.Paths{ProjectDir: root, DepsDir: filepath.Join(root, ".vmake_deps"), CacheDir: filepath.Join(root, "cache")},
	}
	insp, err := pipeline.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	active := insp.PkgDirs["app"].BuildDir
	decoy := filepath.Join(root, "build", "other-configuration")
	for _, dir := range []string{active, decoy} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "artifact.o"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := cleanPackages([]pkgCleanEntry{{Dir: root, Name: "app"}}, ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(active); !os.IsNotExist(err) {
		t.Fatalf("active build directory survived clean: %v", err)
	}
	if _, err := os.Stat(decoy); err != nil {
		t.Fatalf("clean removed an unrelated build directory: %v", err)
	}
}

func TestCleanPackagesKeepsOtherConfigurationsWhenActiveMissing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "build.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	pkg := api.NewPackage().SetName("app").SetRoot(true)
	r := resolver.NewResolver(nil, filepath.Join(root, ".vmake_deps"))
	r.Graph().Packages["app"] = resolver.NewPackageNode("app", buildscript.NewSource("app", path, root, api.SourceLocal), pkg)
	r.Graph().Order = []string{"app"}
	ctx := &RuntimeContext{
		Resolver: r, DepGraph: r.Graph(),
		Config: &config.ConfigFile{Global: &config.GlobalConfig{Toolchain: "host"}},
		Paths:  &pipeline.Paths{ProjectDir: root, DepsDir: filepath.Join(root, ".vmake_deps"), CacheDir: filepath.Join(root, "cache")},
	}
	decoy := filepath.Join(root, "build", "other-configuration")
	if err := os.MkdirAll(decoy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoy, "artifact.o"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := cleanPackages([]pkgCleanEntry{{Dir: root, Name: "app"}}, ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(decoy); err != nil {
		t.Fatalf("clean removed a build directory for another configuration: %v", err)
	}
}

func TestCleanPackagesSkipsUnresolvedPackageToolchain(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "build.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	otherDir := filepath.Join(root, "other")
	if err := os.MkdirAll(otherDir, 0755); err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(otherDir, "build.go")
	if err := os.WriteFile(otherPath, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	app := api.NewPackage().SetName("app").SetRoot(true)
	other := api.NewPackage().SetName("other")
	r := resolver.NewResolver(nil, filepath.Join(root, ".vmake_deps"))
	r.Graph().Packages["app"] = resolver.NewPackageNode("app", buildscript.NewSource("app", path, root, api.SourceLocal), app)
	r.Graph().Packages["other"] = resolver.NewPackageNode("other", buildscript.NewSource("other", otherPath, otherDir, api.SourceLocal), other)
	r.Graph().Order = []string{"app", "other"}
	ctx := &RuntimeContext{
		Resolver: r, DepGraph: r.Graph(),
		Config: &config.ConfigFile{Global: &config.GlobalConfig{Toolchain: "host"}},
		Paths:  &pipeline.Paths{ProjectDir: root, DepsDir: filepath.Join(root, ".vmake_deps"), CacheDir: filepath.Join(root, "cache")},
	}
	config.SetEntry(ctx.Config, "other", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: "clean-missing-toolchain"}})
	if _, err := pipeline.Inspect(ctx); err == nil {
		t.Fatal("strict inspection accepted an unresolved package toolchain")
	}
	insp, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{SkipUnresolvedPackages: true})
	if err != nil {
		t.Fatal(err)
	}
	active := insp.PkgDirs["app"].BuildDir
	otherDecoy := filepath.Join(otherDir, "build", "other-configuration")
	for _, dir := range []string{active, otherDecoy} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "artifact.o"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := cleanPackages([]pkgCleanEntry{{Dir: root, Name: "app"}, {Dir: otherDir, Name: "other"}}, ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(active); !os.IsNotExist(err) {
		t.Fatalf("active build directory survived clean: %v", err)
	}
	if _, err := os.Stat(otherDecoy); err != nil {
		t.Fatalf("clean removed a directory of a package with an unresolved toolchain: %v", err)
	}
}

func TestCleanHooksRecoverScriptError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "build.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	tcName := "clean-" + filepath.Base(filepath.Dir(root))
	copyTC := *host
	copyTC.Name = tcName
	tc := &copyTC
	if err := toolchain.GetManager().RegisterToolchain(tcName, tc); err != nil {
		t.Fatal(err)
	}
	pkg := api.NewPackage().SetName("app").SetRoot(true)
	pkg.OnClean(func(ctx *api.CleanContext) { ctx.String("undeclared") })
	r := resolver.NewResolver(nil, root)
	r.Graph().Packages["app"] = resolver.NewPackageNode("app", buildscript.NewSource("app", path, root, api.SourceLocal), pkg)
	r.Graph().Order = []string{"app"}
	ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Config: &config.ConfigFile{Global: &config.GlobalConfig{Toolchain: tcName}}, Paths: &pipeline.Paths{ProjectDir: root}}
	err = executeCleanHooks(ctx, false, true, true)
	var scriptErr *api.BuildScriptError
	if !errors.As(err, &scriptErr) {
		t.Fatalf("OnClean error was not returned: %v", err)
	}
}

func TestCleanAllRemoteOutputsFindsNestedTrees(t *testing.T) {
	dep := t.TempDir()
	writeTree := func(root string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(root, "out", "owner", "buildkey", "build")
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "artifact.o"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	top := filepath.Join(dep, "native", "root")
	nested := filepath.Join(top, "src", "member")
	writeTree(top)
	writeTree(nested)

	cleanAllRemoteOutputs(dep)

	for _, root := range []string{top, nested} {
		out := filepath.Join(root, "out")
		owners, err := os.ReadDir(out)
		if err != nil {
			t.Fatalf("out under %s missing: %v", root, err)
		}
		for _, owner := range owners {
			builds, err := os.ReadDir(filepath.Join(out, owner.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if len(builds) != 0 {
				t.Fatalf("outputs under %s survived: %v", root, builds)
			}
		}
		if _, err := os.Stat(filepath.Join(root, "src")); err != nil {
			t.Fatalf("source under %s was removed: %v", root, err)
		}
	}
}

func TestValidPkgRefComponent(t *testing.T) {
	for _, good := range []string{"official", "zlib", "a.b", "x-y_1"} {
		if !validPkgRefComponent(good) {
			t.Errorf("validPkgRefComponent(%q) = false", good)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "/abs"} {
		if validPkgRefComponent(bad) {
			t.Errorf("validPkgRefComponent(%q) = true", bad)
		}
	}
}

func TestCleanHooksWithoutFilterRunForUnneededPackages(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "build.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	marker := filepath.Join(root, "unneeded-cleaned")
	app := api.NewPackage().SetName("app").SetRoot(true)
	unneeded := api.NewPackage().SetName("unneeded")
	unneeded.OnClean(func(*api.CleanContext) {
		_ = os.WriteFile(marker, []byte("cleaned"), 0644)
	})
	r := resolver.NewResolver(nil, filepath.Join(root, ".vmake_deps"))
	r.Graph().Packages["app"] = resolver.NewPackageNode("app", buildscript.NewSource("app", path, root, api.SourceLocal), app)
	r.Graph().Packages["unneeded"] = resolver.NewPackageNode("unneeded", buildscript.NewSource("unneeded", path, root, api.SourceLocal), unneeded)
	r.Graph().Order = []string{"app", "unneeded"}
	ctx := &RuntimeContext{
		Resolver: r, DepGraph: r.Graph(),
		Config: &config.ConfigFile{Global: &config.GlobalConfig{Toolchain: "host"}},
		Paths:  &pipeline.Paths{ProjectDir: root, DepsDir: filepath.Join(root, ".vmake_deps"), CacheDir: filepath.Join(root, "cache")},
	}
	if err := executeCleanHooks(ctx, false, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("unneeded package OnClean did not run: %v", err)
	}
}

func buildKeyDirectories(t *testing.T, project string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(project, "build"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			dirs = append(dirs, entry.Name())
		}
	}
	return dirs
}

func TestCleanReportsRemainingConfigurationsAndRemovesCompileDatabase(t *testing.T) {
	project := writeBuildReportProject(t, "int main(void) { return 0; }\n")
	if _, stderr, code := runBuildCommand(t, project, "build"); code != 0 {
		t.Fatalf("debug build exit = %d, stderr = %q", code, stderr)
	}
	if _, stderr, code := runBuildCommand(t, project, "build", "--mode", "release"); code != 0 {
		t.Fatalf("release build exit = %d, stderr = %q", code, stderr)
	}
	database := filepath.Join(project, "build", "compile_commands.json")
	if _, err := os.Stat(database); err != nil {
		t.Fatalf("compile database missing before clean: %v", err)
	}
	before := buildKeyDirectories(t, project)
	if len(before) != 2 {
		t.Fatalf("build key directories before clean = %v, want two configurations", before)
	}

	stdout, stderr, code := runBuildCommand(t, project, "clean")
	if code != 0 {
		t.Fatalf("clean exit = %d, stderr = %q", code, stderr)
	}
	if strings.Count(stdout, "Note:") != 1 || !strings.Contains(stdout, "Note: 1 build director(ies) for other configurations remain") {
		t.Fatalf("clean note missing or duplicated:\n%s", stdout)
	}
	if !strings.Contains(stdout, "'vmake clean --all' or 'vmake distclean'") {
		t.Fatalf("clean note lacks the removal suggestion:\n%s", stdout)
	}
	if after := buildKeyDirectories(t, project); len(after) != 1 || (after[0] != before[0] && after[0] != before[1]) {
		t.Fatalf("clean removed or invented other-configuration keys: before %v, after %v", before, after)
	}
	if _, err := os.Stat(database); !os.IsNotExist(err) {
		t.Fatalf("stale compile database survived clean: %v", err)
	}
	if _, err := os.Stat(reportPath(project)); !os.IsNotExist(err) {
		t.Fatalf("build report survived clean: %v", err)
	}

	stdout, stderr, code = runBuildCommand(t, project, "clean", "--all")
	if code != 0 {
		t.Fatalf("clean --all exit = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "Note:") {
		t.Fatalf("clean --all still warned about remaining configurations:\n%s", stdout)
	}
	if after := buildKeyDirectories(t, project); len(after) != 0 {
		t.Fatalf("clean --all left build key directories: %v", after)
	}
}

func TestRemainingBuildDirectoriesCountsOtherConfigurations(t *testing.T) {
	root := t.TempDir()
	newLocalNode := func(dir, name string) *resolver.PackageNode {
		t.Helper()
		path := filepath.Join(dir, "build.go")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
			t.Fatal(err)
		}
		return resolver.NewPackageNode(name, buildscript.NewSource(name, path, dir, api.SourceLocal), api.NewPackage().SetName(name))
	}
	appDir, extraDir := filepath.Join(root, "app"), filepath.Join(root, "extra")
	r := resolver.NewResolver(nil, filepath.Join(root, ".vmake_deps"))
	r.Graph().Packages["app"] = newLocalNode(appDir, "app")
	r.Graph().Packages["extra"] = newLocalNode(extraDir, "extra")
	r.Graph().Order = []string{"app", "extra"}
	ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph()}
	insp := &pipeline.Inspection{
		Needed: map[string]bool{"app": true},
		PkgDirs: map[string]*api.PkgDirs{
			"app": {SourceDir: appDir, BuildDir: filepath.Join(appDir, "build", "active")},
		},
	}
	for _, dir := range []string{
		filepath.Join(appDir, "build", "active"),
		filepath.Join(appDir, "build", "other"),
		filepath.Join(extraDir, "build", "leftover"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if got := remainingBuildDirectories(ctx, insp); got != 2 {
		t.Fatalf("remaining build directories = %d, want 2", got)
	}
}
