package buildscript

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestScriptHostSelection(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("build.go", `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) { p.SetName(hostName()) }
`)
	selected := "host_" + runtime.GOOS + "_" + runtime.GOARCH + ".go"
	write(selected, "package main\nfunc hostName() string { return \"host\" }\n")
	write("other.go", "//go:build !"+runtime.GOOS+"\n\npackage main\nfunc hostName() string { return \"other\" }\n")
	write("cgo.go", "//go:build cgo\n\npackage main\nfunc hostName() string { return \"cgo\" }\n")
	src := Source{Name: "script", Dir: dir, Path: filepath.Join(dir, "build.go"), Origin: api.SourceLocal}
	pkg, err := LoadBuildScript(src)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Name != "host" {
		t.Fatalf("loaded name = %s", pkg.Name)
	}
	files, err := ListGoFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, len(files))
	for _, file := range files {
		got[filepath.Base(file)] = true
	}
	for _, name := range []string{"build.go", selected} {
		if !got[name] {
			t.Errorf("selected file %s missing from %v", name, files)
		}
	}
	for _, name := range []string{"other.go", "cgo.go"} {
		if got[name] {
			t.Errorf("inactive file %s was selected in %v", name, files)
		}
	}
}

func TestScanSubPackagesLogicalNames(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested", "child")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "build.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sources, err := ScanSubPackages(dir, "repo/parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Name != "repo/parent/nested/child" || sources[0].Dir != nested {
		t.Fatalf("sources = %#v", sources)
	}
}
