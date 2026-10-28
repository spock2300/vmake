package buildscript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectScriptOptionsVmakeJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "buildlib", "layout"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vmake.json"), []byte(`{
		"script": {"module": "example.com/app", "root": "."}
	}`), 0644); err != nil {
		t.Fatal(err)
	}
	opts, err := DetectScriptOptions(filepath.Join(root, "buildlib"), root)
	if err != nil {
		t.Fatal(err)
	}
	if opts.ProjectDir != root || opts.ModulePath != "example.com/app" ||
		opts.ModuleRoot != root {
		t.Fatalf("options = %+v", opts)
	}
}

func TestDetectScriptOptionsGoMod(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(
		"module example.com/fromgomod\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	opts, err := DetectScriptOptions(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if opts.ModulePath != "example.com/fromgomod" || opts.ModuleRoot != root {
		t.Fatalf("options = %+v", opts)
	}
}

func TestScanSubPackagesAttachesScriptOptions(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vmake.json"), []byte(`{
		"script": {"module": "example.com/app"}
	}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "build.go"),
		[]byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sources, err := ScanSubPackages(root, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %d", len(sources))
	}
	if sources[0].Script.ModulePath != "example.com/app" ||
		sources[0].Script.ModuleRoot != root {
		t.Fatalf("script options = %+v", sources[0].Script)
	}
}

func TestDetectScriptOptionsNoConfig(t *testing.T) {
	root := t.TempDir()
	opts, err := DetectScriptOptions(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if opts.ModulePath != "" || opts.ModuleRoot != "" {
		t.Fatalf("options = %+v", opts)
	}
}

func TestDetectScriptOptionsRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vmake.json"), []byte(`{
		"script": {"module": "example.com/app", "root": ".."}
	}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := DetectScriptOptions(root, root); err == nil ||
		!strings.Contains(err.Error(), "leaves") {
		t.Fatalf("escape accepted: %v", err)
	}
}
