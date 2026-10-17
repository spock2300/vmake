package buildscript

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
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

func writeScannerFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestScanSubPackagesDiscoveryRules(t *testing.T) {
	root := t.TempDir()
	writeScannerFile(t, root, "build.go", "package main\n")
	writeScannerFile(t, root, ".git/HEAD", "ref: refs/heads/main\n")
	writeScannerFile(t, root, "alpha/build.go", "package main\n")
	writeScannerFile(t, root, "alpha/beta/build.go", "package main\n")
	writeScannerFile(t, root, "deep/a/b/c/build.go", "package main\n")
	writeScannerFile(t, root, "nested-repo/.git/HEAD", "ref: refs/heads/main\n")
	writeScannerFile(t, root, "nested-repo/build.go", "package main\n")
	writeScannerFile(t, root, "nested-worktree/.git", "gitdir: /tmp/worktree\n")
	writeScannerFile(t, root, "nested-worktree/build.go", "package main\n")
	writeScannerFile(t, root, ".hidden/build.go", "package main\n")
	for _, dir := range []string{"build", "vendor", "node_modules", ".vmake", ".vmake_deps", "vmake_deps"} {
		writeScannerFile(t, root, dir+"/build.go", "package main\n")
	}
	writeScannerFile(t, root, "alpha/Build.go", "package main\n")
	writeScannerFile(t, root, "alpha/build.go.txt", "package main\n")

	sources, err := ScanSubPackages(root, "native/root")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"native/root/alpha":      "alpha",
		"native/root/alpha/beta": "alpha/beta",
		"native/root/deep/a/b/c": "deep/a/b/c",
	}
	if len(sources) != len(want) {
		t.Fatalf("sources = %+v, want %d entries", sources, len(want))
	}
	names := make([]string, 0, len(sources))
	for _, src := range sources {
		names = append(names, src.Name)
		if src.Origin != api.SourceRemote {
			t.Errorf("%s origin = %v, want remote", src.Name, src.Origin)
		}
		if src.Repo != "" {
			t.Errorf("%s repo = %q, want empty before resolver assignment", src.Name, src.Repo)
		}
		if rel, ok := want[src.Name]; !ok {
			t.Errorf("unexpected source %s", src.Name)
		} else if src.Dir != filepath.Join(root, filepath.FromSlash(rel)) {
			t.Errorf("%s dir = %s, want %s", src.Name, src.Dir, filepath.Join(root, filepath.FromSlash(rel)))
		}
	}
	sort.Strings(names)
	wantNames := []string{"native/root/alpha", "native/root/alpha/beta", "native/root/deep/a/b/c"}
	if len(names) != len(wantNames) {
		t.Fatalf("names = %v, want %v", names, wantNames)
	}
	for i := range wantNames {
		if names[i] != wantNames[i] {
			t.Fatalf("names = %v, want %v", names, wantNames)
		}
	}
}

func TestScanSubPackagesMissingRoot(t *testing.T) {
	if _, err := ScanSubPackages(filepath.Join(t.TempDir(), "missing"), "native/root"); err == nil {
		t.Fatal("missing root should return an error")
	}
}

func TestScanLocalNestedScriptsAreTopLevel(t *testing.T) {
	root := t.TempDir()
	writeScannerFile(t, root, "pkg/build.go", "package main\n")
	writeScannerFile(t, root, "pkg/sub/build.go", "package main\n")
	writeScannerFile(t, root, "other/build.go", "package main\n")
	writeScannerFile(t, root, "other/sub/build.go", "package main\n")

	sources, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(sources))
	for _, src := range sources {
		names = append(names, src.Name)
	}
	sort.Strings(names)
	want := []string{"other", "pkg", "sub"}
	if len(names) != len(want) {
		t.Fatalf("local scan names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("local scan names = %v, want %v", names, want)
		}
	}
}
