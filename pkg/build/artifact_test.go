package build

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestArtifactPlacementForSharedTarget(t *testing.T) {
	target := api.NewTargetRegistry().Target("app").SetKind(api.TargetShared).SetInstallDir("custom")
	pkg := api.NewPackage().SetName("app").SetPlatform(api.Platform{OS: "windows"})
	info := &PkgInfo{PkgDirs: api.PkgDirs{SourceDir: "/src", InstallDir: "/stage"}, BuildKey: "key"}
	s := &Scheduler{packages: map[string]*api.Package{"app": pkg}, pkgs: map[string]*PkgInfo{"app": info}}
	resolved := &ResolvedTarget{Node: &BuildNode{PkgName: "app", FullName: "app:app", Target: target}, OutputPath: "build/app.dll"}
	placement := s.placementFor(resolved, info)
	if placement.OutputPath != "build/app.dll" {
		t.Fatalf("output = %q", placement.OutputPath)
	}
	if placement.StageLibDir != filepath.Join("/stage", "lib") || placement.StageIncludeDir != filepath.Join("/stage", "include") {
		t.Fatalf("stage dirs = %q %q", placement.StageLibDir, placement.StageIncludeDir)
	}
	if placement.InstallSubdir != "custom" || placement.InstallName != "libapp.dll" {
		t.Fatalf("install = %q/%q", placement.InstallSubdir, placement.InstallName)
	}
	if placement.ImportLib != filepath.Join("/src", "build/app.dll.a") {
		t.Fatalf("import lib = %q", placement.ImportLib)
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
