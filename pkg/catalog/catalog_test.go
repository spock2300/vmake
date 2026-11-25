package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

func TestBuildPackageSourceUsesCachedNativeSrcDirAsModuleRoot(t *testing.T) {
	depsDir := t.TempDir()
	srcDir := filepath.Join(depsDir, "native", "libfoo", "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module example.com/libfoo\n\ngo 1.26\n"), 0644); err != nil {
		t.Fatal(err)
	}
	buildGo := filepath.Join(srcDir, "build.go")
	if err := os.WriteFile(buildGo, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	src, err := BuildPackageSource(nil, "native", "libfoo", buildGo, "deadbeef", true, false)
	if err != nil {
		t.Fatalf("BuildPackageSource: %v", err)
	}
	if src.Script.ModuleRoot != srcDir || src.Script.ModulePath != "example.com/libfoo" {
		t.Fatalf("Script = %+v, want ModuleRoot=%q ModulePath=example.com/libfoo", src.Script, srcDir)
	}
}

func TestEnvLoadScriptPrefersInjectedLoader(t *testing.T) {
	called := false
	env := Env{LoadScript: func(buildscript.Source) (*api.Package, error) {
		called = true
		return api.NewPackage().SetName("fake"), nil
	}}
	pkg, err := env.loadScript(buildscript.Source{})
	if err != nil {
		t.Fatalf("loadScript: %v", err)
	}
	if !called || pkg == nil {
		t.Fatalf("injected loader was not used: called=%v pkg=%v", called, pkg)
	}
}

func TestCatalogScriptUsesTheInjectedLoader(t *testing.T) {
	c := Open(Env{LoadScript: func(buildscript.Source) (*api.Package, error) {
		return nil, errors.New("interpretation failed")
	}})
	if _, err := c.Script(buildscript.Source{}); err == nil {
		t.Fatal("Catalog.Script swallowed the loader error")
	}
}

func TestShowRejectsAnEnvironmentWithoutARepositoryManager(t *testing.T) {
	if _, _, err := Open(Env{}).Show("missing", "pkg", ""); err == nil {
		t.Fatal("Show accepted an environment with no repository manager")
	}
}
