package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/buildscript"
)

func TestCheckAutoWireNoRequires(t *testing.T) {
	dir := t.TempDir()
	src := makeBuildGoInDir(dir, "clean", `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("*.c")
	})
}`)
	findings := checkAutoWire(src)
	if len(findings) != 0 {
		t.Errorf("no AddRequires → no findings, got %v", findings)
	}
}

func TestCheckAutoWireWithAddDeps(t *testing.T) {
	dir := t.TempDir()
	src := makeBuildGoInDir(dir, "explicit", `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddDeps("lib:lib")
	})
}`)
	findings := checkAutoWire(src)
	if len(findings) != 0 {
		t.Errorf("explicit AddDeps → no findings, got %v", findings)
	}
}

func TestCheckAutoWireMissingAddDeps(t *testing.T) {
	dir := t.TempDir()
	src := makeBuildGoInDir(dir, "implicit", `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("*.c")
	})
}`)
	findings := checkAutoWire(src)
	if len(findings) != 1 {
		t.Fatalf("implicit dep → 1 finding, got %d", len(findings))
	}
	if findings[0].Category != "autoWire" {
		t.Errorf("category = %q", findings[0].Category)
	}
	if findings[0].Severity != "warn" {
		t.Errorf("severity = %q", findings[0].Severity)
	}
}

func TestCheckAutoWireOnRequireWithoutAddRequires(t *testing.T) {
	dir := t.TempDir()
	src := makeBuildGoInDir(dir, "empty-require", `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary)
	})
}`)
	findings := checkAutoWire(src)
	// OnRequire present but no AddRequires — should not warn (no deps to wire)
	if len(findings) != 0 {
		t.Errorf("OnRequire without AddRequires → no findings, got %v", findings)
	}
}

func TestHasSetRootTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "build.go")
	_ = os.WriteFile(path, []byte(`package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) { p.SetRoot(true) }`), 0644)
	if !hasSetRoot(path) {
		t.Error("hasSetRoot should detect SetRoot(true)")
	}
}

func TestHasSetRootFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "build.go")
	_ = os.WriteFile(path, []byte(`package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) { p.OnBuild(func(ctx *api.BuildContext) {}) }`), 0644)
	if hasSetRoot(path) {
		t.Error("hasSetRoot should return false when no SetRoot(true)")
	}
}

func TestHasSetRootMissingFile(t *testing.T) {
	if hasSetRoot("/nonexistent/build.go") {
		t.Error("hasSetRoot should return false for missing file")
	}
}

func TestCheckAutoWireMissingFile(t *testing.T) {
	src := buildscript.Source{Path: "/nonexistent/build.go"}
	findings := checkAutoWire(src)
	if len(findings) != 0 {
		t.Errorf("missing file → no findings, got %v", findings)
	}
}

func makeBuildGoInDir(dir, pkgName, content string) buildscript.Source {
	path := filepath.Join(dir, "build.go")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		panic(err)
	}
	return buildscript.Source{
		Name: pkgName,
		Path: path,
		Dir:  dir,
	}
}
