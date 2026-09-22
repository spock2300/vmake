package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func testToolchain() *toolchain.Toolchain {
	return &toolchain.Toolchain{
		Name: "test",
		Tools: toolchain.Tools{
			CC:  "cc",
			CXX: "c++",
			AR:  "ar",
			LD:  "cc",
		},
	}
}

func TestDeclarePackageTargetsDryRunWiring(t *testing.T) {
	node := localNode("pkg")
	var sawDryRun bool
	var sawBuildDir string
	var sawCC string
	var depOutput = "sentinel"
	node.Pkg.OnBuild(func(ctx *api.BuildContext) {
		sawDryRun = node.Pkg.DryRun()
		sawBuildDir = node.Pkg.BuildDir()
		sawCC = node.Pkg.CC()
		depOutput = ctx.DepOutput("other:tgt")
		ctx.BuildSubGraph("other")
		ctx.Exec("vmake-definitely-missing-cmd-xyz")
		ctx.Target("app").SetKind(api.TargetBinary)
	})

	ctx := &RuntimeContext{
		Config:   &config.ConfigFile{},
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{"pkg": node}},
	}
	srcDir := t.TempDir()
	dirs := &api.PkgDirs{SourceDir: srcDir, BuildDir: filepath.Join(srcDir, "build", "key1")}

	buildCtx, err := DeclareTargets(ctx, "pkg", dirs, testToolchain(), map[string]any{"mode": "debug"})
	if err != nil {
		t.Fatal(err)
	}

	if !sawDryRun {
		t.Error("package must be in dry-run mode during OnBuild declarations")
	}
	if sawBuildDir != dirs.BuildDir {
		t.Errorf("BuildDir during declaration = %q, want %q", sawBuildDir, dirs.BuildDir)
	}
	if sawCC != "cc" {
		t.Errorf("CC during declaration = %q, want toolchain wired", sawCC)
	}
	if depOutput != "" {
		t.Errorf("DepOutput in dry-run = %q, want empty stub", depOutput)
	}
	if node.Pkg.DryRun() {
		t.Error("package dry-run must be restored to false after declarations")
	}
	if _, ok := buildCtx.GetTargets()["app"]; !ok {
		t.Error("declared target app missing from returned BuildContext")
	}
}

func TestDeclarePackageTargetsSkipsConfigDefines(t *testing.T) {
	node := localNode("pkg")
	node.Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.GenerateConfigDefines()
		ctx.ImportConfig("dep")
		ctx.Target("app").SetKind(api.TargetBinary)
	})

	ctx := &RuntimeContext{
		Config: &config.ConfigFile{},
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"pkg": node,
		}},
	}

	srcDir := t.TempDir()
	dirs := &api.PkgDirs{SourceDir: srcDir, BuildDir: filepath.Join(srcDir, "build", "key1")}
	buildCtx, err := DeclareTargets(ctx, "pkg", dirs, testToolchain(), map[string]any{"mode": "debug"})
	if err != nil {
		t.Fatal(err)
	}

	target := buildCtx.GetTargets()["app"]
	if target == nil {
		t.Fatal("target app missing")
	}
	if len(target.Defines()) != 0 {
		t.Errorf("dry-run declaration must not inject config defines, got %v", target.Defines())
	}
}
