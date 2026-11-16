package catalog

import (
	"os"
	"path/filepath"
	"testing"
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
