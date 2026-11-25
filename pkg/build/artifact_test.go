package build

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestArtifactPlacementForSharedTarget(t *testing.T) {
	target := api.NewTargetRegistry().Target("app").SetKind(api.TargetShared).SetInstallDir("custom")
	pkg := api.NewPackage().SetName("app").SetPlatform(api.Platform{OS: "windows"})
	info := &PkgInfo{PkgDirs: api.PkgDirs{SourceDir: "/src", InstallDir: "/stage"}}
	s := &Scheduler{packages: map[string]*api.Package{"app": pkg}, pkgs: map[string]*PkgInfo{"app": info}}
	resolved := &ResolvedTarget{Node: &BuildNode{PkgName: "app", FullName: "app:app", Target: target}, OutputPath: "build/app.dll"}
	placement := s.placementFor(resolved, info)
	if want := filepath.Join("build", "libapp.dll"); placement.OutputPath != want {
		t.Fatalf("output = %q, want %q", placement.OutputPath, want)
	}
	if placement.StageLibDir != filepath.Join("/stage", "lib") || placement.StageIncludeDir != filepath.Join("/stage", "include") {
		t.Fatalf("stage dirs = %q %q", placement.StageLibDir, placement.StageIncludeDir)
	}
	if placement.InstallSubdir != "custom" || placement.InstallName != "libapp.dll" {
		t.Fatalf("install = %q/%q", placement.InstallSubdir, placement.InstallName)
	}
	if want := filepath.Join("/src", "build", "libapp.dll.a"); placement.ImportLib != want {
		t.Fatalf("import lib = %q, want %q", placement.ImportLib, want)
	}
	if placement.Kind != api.TargetShared || placement.IsTest {
		t.Fatalf("placement = %+v", placement)
	}
}

func TestArtifactPlacementForObjectUsesKindDir(t *testing.T) {
	target := api.NewTargetRegistry().Target("obj").SetKind(api.TargetObject)
	pkg := api.NewPackage().SetName("app").SetPlatform(api.Platform{OS: "linux"})
	info := &PkgInfo{PkgDirs: api.PkgDirs{SourceDir: "/src", InstallDir: "/stage"}}
	s := &Scheduler{packages: map[string]*api.Package{"app": pkg}, pkgs: map[string]*PkgInfo{"app": info}}
	resolved := &ResolvedTarget{Node: &BuildNode{PkgName: "app", FullName: "app:obj", Target: target}, OutputPath: "build/obj.o"}
	placement := s.placementFor(resolved, info)
	if placement.ImportLib != "" {
		t.Fatalf("object import lib = %q", placement.ImportLib)
	}
	if placement.InstallName != "obj.o" {
		t.Fatalf("install name = %q", placement.InstallName)
	}
}

func TestArtifactPlacementPrebuiltSharedHasNoImportLibrary(t *testing.T) {
	info := &PkgInfo{PkgDirs: api.PkgDirs{SourceDir: "/src"}}

	built := PlacementFor(api.NewTargetRegistry().Target("app").SetKind(api.TargetShared), info, "windows")
	if want := filepath.Join("/src", "build", "libapp.dll.a"); built.ImportLib != want {
		t.Fatalf("built import lib = %q, want %q", built.ImportLib, want)
	}

	prebuilt := PlacementFor(api.NewTargetRegistry().Target("app").SetKind(api.TargetShared).SetPrebuilt("shipped/libapp.dll"), info, "windows")
	if prebuilt.ImportLib != "" {
		t.Fatalf("prebuilt import lib = %q, want none", prebuilt.ImportLib)
	}
	if prebuilt.OutputPath != built.OutputPath {
		t.Fatalf("prebuilt output = %q, want %q", prebuilt.OutputPath, built.OutputPath)
	}
}
