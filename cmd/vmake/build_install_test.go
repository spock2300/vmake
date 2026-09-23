package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestInstallReusesBuildDeclaration(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "data.txt")
	if err := os.WriteFile(input, []byte("declared once"), 0644); err != nil {
		t.Fatal(err)
	}
	var declarations, installs int
	pkg := api.NewPackage().SetName("app")
	pkg.OnBuild(func(ctx *api.BuildContext) {
		declarations++
		ctx.Target("data").SetKind(api.TargetVoid)
		ctx.AddInstalls("data.txt", "share/data.txt")
	})
	pkg.OnInstall(func(*api.InstallContext) { installs++ })
	declared := api.NewBuildContext("app", nil)
	pkg.ExecBuildFuncs(root, func(fn api.BuildFunc) { fn(declared) })
	targets := map[string]map[string]*api.Target{"app": declared.GetTargets()}
	graph, err := build.NewBuildGraph(targets, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &RuntimeContext{Config: &config.ConfigFile{}, AllOptions: map[string]map[string]*api.Option{}}
	dirs := map[string]*api.PkgDirs{"app": {SourceDir: root, BuildDir: filepath.Join(root, "build")}}
	result := &BuildResult{AllTargets: targets, Graph: graph, PkgDirs: dirs, BuildCtxs: map[string]*api.BuildContext{"app": declared}}
	installer := build.NewArtifactInstaller(graph, dirs, filepath.Join(root, "install"))
	if err := installOnePackage(ctx, "app", &resolver.PackageNode{Pkg: pkg}, result, installer, nil); err != nil {
		t.Fatal(err)
	}
	if err := installer.InstallAll(nil); err != nil {
		t.Fatal(err)
	}
	if declarations != 1 || installs != 1 {
		t.Fatalf("OnBuild=%d OnInstall=%d", declarations, installs)
	}
	if data, err := os.ReadFile(filepath.Join(root, "install", "share", "data.txt")); err != nil || string(data) != "declared once" {
		t.Fatalf("declaration install item: %q, %v", data, err)
	}
}
