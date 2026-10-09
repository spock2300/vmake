package main

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestInstallPrefixPriority(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cliFlag bool
	}{
		{"package prefix without cli flag", false},
		{"cli prefix overrides package prefix", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			custom := filepath.Join(root, "custom-prefix")
			defaultPrefix := filepath.Join(root, "install")
			cliPrefix := filepath.Join(root, "cli-prefix")
			for _, file := range []struct{ name, content string }{{"data.txt", "extra data"}, {"app-input", "binary bytes"}} {
				if err := os.WriteFile(filepath.Join(root, file.name), []byte(file.content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			buildDir := filepath.Join(root, "build")
			if err := os.MkdirAll(buildDir, 0755); err != nil {
				t.Fatal(err)
			}
			appName := api.TargetFilename(api.TargetBinary, "app", runtime.GOOS)
			if err := os.WriteFile(filepath.Join(buildDir, appName), []byte("binary bytes"), 0755); err != nil {
				t.Fatal(err)
			}

			installerPrefix := defaultPrefix
			if tc.cliFlag {
				previous := prefixFlag
				prefixFlag = cliPrefix
				t.Cleanup(func() { prefixFlag = previous })
				installerPrefix = cliPrefix
			}

			pkg := api.NewPackage().SetName("app")
			pkg.OnBuild(func(ctx *api.BuildContext) {
				ctx.Target("app").SetKind(api.TargetBinary).SetPrebuilt("app-input")
				ctx.AddInstalls("data.txt", "share/data.txt")
			})
			pkg.OnInstall(func(ctx *api.InstallContext) { ctx.SetPrefix(custom) })

			declared := api.NewBuildContext("app", nil)
			pkg.ExecBuildFuncs(root, func(fn api.BuildFunc) { fn(declared) })
			targets := map[string]map[string]*api.Target{"app": declared.GetTargets()}
			graph, err := build.NewBuildGraph(targets, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx := &RuntimeContext{Config: &config.ConfigFile{}, AllOptions: map[string]map[string]*api.Option{}}
			dirs := map[string]*api.PkgDirs{"app": {SourceDir: root, BuildDir: buildDir}}
			result := &BuildResult{AllTargets: targets, Graph: graph, PkgDirs: dirs, BuildCtxs: map[string]*api.BuildContext{"app": declared}}
			installer := build.NewArtifactInstaller(graph, dirs, installerPrefix)
			if err := installOnePackage(ctx, "app", &resolver.PackageNode{Pkg: pkg}, result, installer, nil); err != nil {
				t.Fatal(err)
			}
			if err := installer.InstallAll(nil); err != nil {
				t.Fatal(err)
			}

			want := custom
			unused := defaultPrefix
			if tc.cliFlag {
				want, unused = cliPrefix, custom
			}
			for _, rel := range []string{filepath.Join("bin", appName), filepath.Join("share", "data.txt")} {
				if _, err := os.Stat(filepath.Join(want, rel)); err != nil {
					t.Fatalf("expected %s under %s: %v", rel, want, err)
				}
			}
			if _, err := os.Stat(unused); !os.IsNotExist(err) {
				t.Fatalf("unexpected install under %s: %v", unused, err)
			}
		})
	}
}
