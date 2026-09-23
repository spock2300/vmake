package main

import (
	"errors"
	"os"
	"path/filepath"
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
	r := resolver.NewResolver(nil, filepath.Join(root, "vmake_deps"))
	r.Graph().Packages["app"] = resolver.NewPackageNode("app", buildscript.NewSource("app", path, root, api.SourceLocal), pkg)
	r.Graph().Order = []string{"app"}
	ctx := &RuntimeContext{
		Resolver: r, DepGraph: r.Graph(),
		Config: &config.ConfigFile{Global: &config.GlobalConfig{Toolchain: "host"}},
		Paths:  &pipeline.Paths{ProjectDir: root, DepsDir: filepath.Join(root, "vmake_deps"), CacheDir: filepath.Join(root, "cache")},
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
	r := resolver.NewResolver(nil, filepath.Join(root, "vmake_deps"))
	r.Graph().Packages["app"] = resolver.NewPackageNode("app", buildscript.NewSource("app", path, root, api.SourceLocal), pkg)
	r.Graph().Order = []string{"app"}
	ctx := &RuntimeContext{
		Resolver: r, DepGraph: r.Graph(),
		Config: &config.ConfigFile{Global: &config.GlobalConfig{Toolchain: "host"}},
		Paths:  &pipeline.Paths{ProjectDir: root, DepsDir: filepath.Join(root, "vmake_deps"), CacheDir: filepath.Join(root, "cache")},
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
	r := resolver.NewResolver(nil, filepath.Join(root, "vmake_deps"))
	r.Graph().Packages["app"] = resolver.NewPackageNode("app", buildscript.NewSource("app", path, root, api.SourceLocal), app)
	r.Graph().Packages["other"] = resolver.NewPackageNode("other", buildscript.NewSource("other", otherPath, otherDir, api.SourceLocal), other)
	r.Graph().Order = []string{"app", "other"}
	ctx := &RuntimeContext{
		Resolver: r, DepGraph: r.Graph(),
		Config: &config.ConfigFile{Global: &config.GlobalConfig{Toolchain: "host"}},
		Paths:  &pipeline.Paths{ProjectDir: root, DepsDir: filepath.Join(root, "vmake_deps"), CacheDir: filepath.Join(root, "cache")},
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
	err = executeCleanHooks(ctx, false, true)
	var scriptErr *api.BuildScriptError
	if !errors.As(err, &scriptErr) {
		t.Fatalf("OnClean error was not returned: %v", err)
	}
}
