package build

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestArtifactInstallerConsumesPlacement(t *testing.T) {
	source := t.TempDir()
	target := api.NewTargetRegistry().Target("app").SetKind(api.TargetBinary)
	info := &PkgInstallInfo{
		TargetOS: "linux",
		Placements: map[string]ArtifactPlacement{
			"app": {
				OutputPath:    "out/custom-app",
				ImportLib:     filepath.Join(source, "out", "custom-app.a"),
				InstallSubdir: "bin",
				InstallName:   "renamed",
			},
		},
	}
	installer := NewArtifactInstaller(&BuildGraph{}, map[string]*api.PkgDirs{
		"p": {SourceDir: source, BuildDir: filepath.Join(source, "build")},
	}, t.TempDir())

	if got, want := installer.getOutputPath("p", info, &BuildNode{Target: target}), filepath.Join(source, "out", "custom-app"); got != want {
		t.Fatalf("output path = %q, want %q", got, want)
	}
	if got, want := installer.getInstallPath("prefix", info, target), filepath.Join("prefix", "bin", "renamed"); got != want {
		t.Fatalf("install path = %q, want %q", got, want)
	}
}

func TestArtifactInstallerFallsBackWithoutPlacement(t *testing.T) {
	source := t.TempDir()
	target := api.NewTargetRegistry().Target("app").SetKind(api.TargetBinary).SetInstallDir("sbin")
	info := &PkgInstallInfo{TargetOS: "linux", BuildDir: filepath.Join(source, "build")}
	installer := NewArtifactInstaller(&BuildGraph{}, map[string]*api.PkgDirs{
		"p": {SourceDir: source, BuildDir: filepath.Join(source, "build")},
	}, t.TempDir())

	if got, want := installer.getOutputPath("p", info, &BuildNode{Target: target}), filepath.Join(source, "build", "app"); got != want {
		t.Fatalf("output path = %q, want %q", got, want)
	}
	if got, want := installer.getInstallPath("prefix", info, target), filepath.Join("prefix", "sbin", "app"); got != want {
		t.Fatalf("install path = %q, want %q", got, want)
	}
}
