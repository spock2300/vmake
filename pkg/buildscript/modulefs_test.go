package buildscript

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleFSReadsModuleAndRejectsOutside(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "helper.go"), []byte("package helper\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gopath := filepath.Join(t.TempDir(), "gopath")
	prefix := filepath.Join(gopath, "src", "example.com", "app")
	fsys := moduleFS{prefix: prefix, root: root}

	data, err := fs.ReadFile(fsys, filepath.Join(prefix, "helper.go"))
	if err != nil || string(data) != "package helper\n" {
		t.Fatalf("module read = %q, %v", data, err)
	}
	if _, err := fs.Stat(fsys, filepath.Join(prefix, "missing.go")); err == nil {
		t.Fatal("missing module file was accepted")
	}
	if _, err := fs.ReadFile(fsys, filepath.Join(root, "helper.go")); err == nil {
		t.Fatal("path outside the virtual GOPATH was accepted")
	}
}

func TestModuleFSRejectsEscape(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(t.TempDir(), "gopath", "src", "example.com", "app")
	fsys := moduleFS{prefix: prefix, root: root}
	if _, err := fsys.Open(filepath.Join(prefix, "..", "outside.go")); err == nil {
		t.Fatal("parent path was accepted")
	}
}
