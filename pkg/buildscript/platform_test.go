package buildscript

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestScriptHostSelectionAndHash(t *testing.T) {
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
	ignored := "//go:build !" + runtime.GOOS + "\n\npackage main\nfunc hostName() string { return \"other\" }\n"
	write("other.go", ignored)
	write("cgo.go", "//go:build cgo\n\npackage main\nfunc hostName() string { return \"cgo\" }\n")
	src := Source{Name: "script", Dir: dir, Path: filepath.Join(dir, "build.go"), Origin: api.SourceLocal}
	pkg, err := LoadBuildScript(src)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Name != "host" {
		t.Fatalf("loaded name = %s", pkg.Name)
	}
	before, err := ScriptSetHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	write("other.go", ignored+"\nvar ignoredValue = 1\n")
	afterIgnored, err := ScriptSetHash(dir)
	if err != nil || afterIgnored != before {
		t.Fatalf("ignored edit changed hash: %s -> %s, %v", before, afterIgnored, err)
	}
	write(selected, "package main\nfunc hostName() string { return \"changed\" }\n")
	afterSelected, err := ScriptSetHash(dir)
	if err != nil || afterSelected == before {
		t.Fatalf("selected edit did not change hash: %s -> %s, %v", before, afterSelected, err)
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
