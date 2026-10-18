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

func TestValidateInstallPrefixRejectsDestructiveTargets(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"", project, root, filepath.Dir(root)} {
		if err := validateInstallPrefix(prefix, project); err == nil {
			t.Errorf("validateInstallPrefix(%q) accepted a destructive target", prefix)
		}
	}
	if err := validateInstallPrefix(filepath.Join(project, "src"), project); err == nil {
		t.Errorf("project subdirectory accepted as install prefix")
	}
	if cwd, err := os.Getwd(); err == nil {
		if err := validateInstallPrefix(cwd, project); err == nil {
			t.Errorf("current working directory accepted as install prefix")
		}
	}
	if err := validateInstallPrefix(filepath.Join(project, "install"), project); err != nil {
		t.Errorf("project-local prefix rejected: %v", err)
	}
	if err := validateInstallPrefix(filepath.Join(project, "install", "nested"), project); err != nil {
		t.Errorf("default install subdirectory rejected: %v", err)
	}
	storage := filepath.Join(root, "vmake-state")
	if err := os.MkdirAll(filepath.Join(storage, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := validateInstallPrefix(filepath.Join(storage, "cache"), project, storage); err == nil {
		t.Error("vmake storage subdirectory accepted as install prefix")
	}
	regular := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(regular, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateInstallPrefix(regular, project); err == nil {
		t.Error("regular file accepted as install prefix")
	}
	nonEmpty := filepath.Join(root, "existing-custom")
	if err := os.MkdirAll(nonEmpty, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "keep"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateInstallPrefix(nonEmpty, project); err == nil {
		t.Error("non-empty custom install prefix accepted without an ownership marker")
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, installOwnershipMarker), []byte("vmake\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateInstallPrefix(nonEmpty, project); err != nil {
		t.Errorf("owned custom install prefix rejected: %v", err)
	}
	empty := filepath.Join(root, "empty-custom")
	if err := os.MkdirAll(empty, 0755); err != nil {
		t.Fatal(err)
	}
	if err := validateInstallPrefix(empty, project); err != nil {
		t.Errorf("empty custom install prefix rejected: %v", err)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if err := validateInstallPrefix(home, project); err == nil {
			t.Errorf("home directory accepted as install prefix")
		}
	}
	if runtime.GOOS != "windows" {
		if err := validateInstallPrefix(string(filepath.Separator), project); err == nil {
			t.Errorf("filesystem root accepted as install prefix")
		}
	}
}

func TestValidateInstallPrefixFollowsSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{project, outside} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "install-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := validateInstallPrefix(link, project); err != nil {
		t.Fatalf("symlinked install directory rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateInstallPrefix(link, project); err == nil {
		t.Error("non-empty symlinked install directory accepted without an ownership marker")
	}
	fileTarget := filepath.Join(root, "file-target")
	if err := os.WriteFile(fileTarget, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(root, "file-link")
	if err := os.Symlink(fileTarget, fileLink); err != nil {
		t.Fatal(err)
	}
	if err := validateInstallPrefix(fileLink, project); err == nil {
		t.Error("symlink to a regular file accepted as install prefix")
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
