package pipeline

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestInspectMatchesPackageToolchainBuildDirectories(t *testing.T) {
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
	config.SetEntry(s.ctx.Config, "dep", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: name}})
	s.ctx.DepGraph.Packages["app"].Pkg.SetRoot(true)
	s.ctx.DepGraph.Packages["app"].Deps = []string{"dep"}
	s.cfg = makeBuildConfig(s.ctx, host, "host")
	s.globalFlagsHash = build.GlobalFlagsHash()
	s.computeDirsAndOptions()
	if err := s.prepareAllPackages(); err != nil {
		t.Fatal(err)
	}
	inspection, err := Inspect(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"app", "dep"} {
		if !reflect.DeepEqual(inspection.PkgDirs[pkg], s.pkgDirs[pkg]) {
			t.Errorf("%s inspection/build directories differ: %+v / %+v", pkg, inspection.PkgDirs[pkg], s.pkgDirs[pkg])
		}
	}
	if inspection.PackageToolchains["dep"] != tc || inspection.PackageToolchains["app"] != host {
		t.Fatal("inspection lost per-package toolchain selection")
	}
	missing := name + "-missing"
	called := false
	mgr.SetOnMissing(missing, func(string) (*toolchain.Toolchain, error) { called = true; return tc, nil })
	config.SetEntry(s.ctx.Config, "dep", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: missing}})
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
	s.ctx.DepGraph.Packages["app"].Pkg.SetRoot(true)
	missing := "inspect-skip-" + storage.OwnerKey(s.ctx.Paths.ProjectDir)[:16]
	config.SetEntry(s.ctx.Config, "dep", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: missing}})

	if _, err := Inspect(s.ctx); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("strict inspection did not report the missing toolchain: %v", err)
	}
	inspection, err := InspectWithOptions(s.ctx, InspectOptions{SkipUnresolvedPackages: true})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.PkgDirs["dep"] != nil || inspection.PackageToolchains["dep"] != nil {
		t.Fatalf("unresolved package was not skipped: %+v", inspection.PkgDirs["dep"])
	}
	if inspection.PkgDirs["app"] == nil || inspection.PackageToolchains["app"] != host {
		t.Fatal("skipping an unresolved package dropped resolved packages")
	}
}

func TestInspectMatchesNativeMemberSourceLinks(t *testing.T) {
	for _, member := range []string{"src", "src/sub", "out", "normal", "nested/member"} {
		for _, setGit := range []bool{false, true} {
			label := member
			if setGit {
				label += "/setgit"
			}
			t.Run(label, func(t *testing.T) {
				s, _, name := nativeMemberLinkFixture(t, member, setGit)
				if err := s.setupSubPackageDirs(s.ctx.Paths.DepsDir); err != nil {
					t.Fatal(err)
				}
				if err := s.cloneSubPackageGitSources(); err != nil {
					t.Fatal(err)
				}
				node := s.ctx.DepGraph.Packages[name]
				sourceDir := node.Pkg.SrcDir()
				node.Pkg.SetSrcDir("").SetDirs(api.PkgDirs{})
				inspection, err := Inspect(s.ctx)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(inspection.PkgDirs[name], s.pkgDirs[name]) {
					t.Fatalf("inspection/build directories differ: %+v / %+v", inspection.PkgDirs[name], s.pkgDirs[name])
				}
				if setGit && node.Pkg.SrcDir() != sourceDir {
					t.Fatalf("inspected SetGit source = %s, want %s", node.Pkg.SrcDir(), sourceDir)
				}
			})
		}
	}
}
