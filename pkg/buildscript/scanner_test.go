package buildscript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSubPackagesSkipsNestedRepositories(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("build.go", "package main\n")
	write("member/build.go", "package main\n")
	// A native member's own SetGit working tree is a nested repository; its
	// upstream build.go must not be discovered as a sub-package.
	write("member/src/.git/HEAD", "ref: refs/heads/main\n")
	write("member/src/build.go", "package main\n")

	sources, err := ScanSubPackages(root, "native/root")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Name != "native/root/member" {
		t.Fatalf("sources = %+v, want only native/root/member", sources)
	}
}
