package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

// installInfoFor builds the PkgInstallInfo a build session produces: one
// placement per target, computed by PlacementFor rather than re-derived.
func installInfoFor(pkgName string, graph *BuildGraph, dirs *api.PkgDirs, buildKey, targetOS string) *PkgInstallInfo {
	pkgInfo := &PkgInfo{PkgDirs: *dirs, BuildKey: buildKey}
	info := &PkgInstallInfo{
		BuildDir:   dirs.BuildDir,
		TargetOS:   targetOS,
		Placements: make(map[string]ArtifactPlacement),
	}
	for _, node := range graph.Nodes {
		if node.PkgName != pkgName || node.Target == nil {
			continue
		}
		info.Placements[node.Target.Name()] = PlacementFor(node.Target, pkgInfo, targetOS)
	}
	return info
}

func TestArtifactInstallerConsumesPlacement(t *testing.T) {
	source := t.TempDir()
	placement := ArtifactPlacement{
		OutputPath:    "out/custom-app",
		ImportLib:     filepath.Join(source, "out", "custom-app.a"),
		InstallSubdir: "bin",
		InstallName:   "renamed",
	}
	installer := NewArtifactInstaller(&BuildGraph{}, map[string]*api.PkgDirs{
		"p": {SourceDir: source, BuildDir: filepath.Join(source, "build")},
	}, t.TempDir())

	if got, want := installer.getOutputPath("p", placement), filepath.Join(source, "out", "custom-app"); got != want {
		t.Fatalf("output path = %q, want %q", got, want)
	}
	if got, want := installer.getInstallPath("prefix", placement), filepath.Join("prefix", "bin", "renamed"); got != want {
		t.Fatalf("install path = %q, want %q", got, want)
	}
}

// A target without a placement was not produced by this build, so it must not
// be installed even when a file with the derived name happens to exist.
func TestArtifactInstallerSkipsTargetsWithoutPlacement(t *testing.T) {
	source := t.TempDir()
	buildDir := filepath.Join(source, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "app"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := api.NewTargetRegistry().Target("app").SetKind(api.TargetBinary).SetDefault(true)
	targets := map[string]map[string]*api.Target{"p": {"app": target}}
	graph, err := NewBuildGraph(targets, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(source, "install")
	installer := NewArtifactInstaller(graph, map[string]*api.PkgDirs{
		"p": {SourceDir: source, BuildDir: buildDir},
	}, prefix)
	installer.SetPackageInfo("p", &PkgInstallInfo{BuildDir: buildDir, TargetOS: "linux"})
	if err := installer.InstallAll(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(prefix, "bin", "app")); !os.IsNotExist(err) {
		t.Fatalf("target without a placement was installed: %v", err)
	}
}

// A void target is an action, not an artifact: even a same-named file in the
// build directory must not be installed.
func TestArtifactInstallerSkipsVoidTargets(t *testing.T) {
	source := t.TempDir()
	buildDir := filepath.Join(source, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "gen"), []byte("codegen"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := api.NewTargetRegistry().Target("gen").SetKind(api.TargetVoid).SetDefault(true)
	targets := map[string]map[string]*api.Target{"p": {"gen": target}}
	graph, err := NewBuildGraph(targets, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dirs := &api.PkgDirs{SourceDir: source, BuildDir: buildDir}
	prefix := filepath.Join(source, "install")
	installer := NewArtifactInstaller(graph, map[string]*api.PkgDirs{"p": dirs}, prefix)
	// Even a placement that looks installable must be ignored for a void target:
	// the kind check, not the placement, is what protects generated files.
	installer.SetPackageInfo("p", &PkgInstallInfo{
		BuildDir: buildDir,
		TargetOS: "linux",
		Placements: map[string]ArtifactPlacement{
			"gen": {OutputPath: filepath.Join(buildDir, "gen"), InstallSubdir: "bin", InstallName: "gen"},
		},
	})
	if err := installer.InstallAll(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(prefix, "gen")); !os.IsNotExist(err) {
		t.Fatalf("void target output was installed: %v", err)
	}
}
