package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/internal/fs"
)

func cleanupStorageFixture(t *testing.T) string {
	t.Helper()
	origVmakeDir := vmakeDir
	vmakeDir = t.TempDir()
	t.Cleanup(func() { vmakeDir = origVmakeDir })
	t.Setenv("VMAKE_CACHE", t.TempDir())

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "build.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

func TestCleanupLegacyStorageKeepsPreFetchedTrees(t *testing.T) {
	root := cleanupStorageFixture(t)
	trees := filepath.Join(root, ".vmake_deps", "local", "app", "src")
	if err := os.MkdirAll(trees, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "vmake_deps")
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}

	cleanupLegacyStorage()

	if !fs.FileExists(trees) {
		t.Fatal(".vmake_deps was removed before the first build")
	}
	if fs.FileExists(legacy) {
		t.Fatal("legacy vmake_deps survived the upgrade")
	}
	if data, err := os.ReadFile(filepath.Join(root, ".vmake", "layout")); err != nil || string(data) != storageLayoutVersion {
		t.Fatalf("layout marker = %q, %v", data, err)
	}
}

func TestCleanupLegacyStorageKeepsV3Trees(t *testing.T) {
	root := cleanupStorageFixture(t)
	trees := filepath.Join(root, ".vmake_deps", "native", "root", "src")
	if err := os.MkdirAll(trees, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".vmake"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".vmake", "layout"), []byte("3"), 0644); err != nil {
		t.Fatal(err)
	}

	cleanupLegacyStorage()

	if !fs.FileExists(trees) {
		t.Fatal("v3 tree store was removed on upgrade to v4")
	}
	if data, err := os.ReadFile(filepath.Join(root, ".vmake", "layout")); err != nil || string(data) != storageLayoutVersion {
		t.Fatalf("layout marker = %q, %v", data, err)
	}
}

func TestRequiresExclusiveStorage(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"build"}, false},
		{[]string{"test"}, false},
		{[]string{"query", "targets"}, false},
		{[]string{"manifest", "checkout", "manifest.json"}, false},
		{[]string{"clean"}, true},
		{[]string{"distclean"}, true},
		{[]string{"rebuild"}, true},
		{[]string{"lock", "update"}, true},
		{[]string{"pkg", "clean", "official/zlib"}, true},
		{[]string{"pkg", "update", "official/zlib"}, true},
	}
	for _, tc := range cases {
		cmd, _, err := RootCmd.Find(tc.args)
		if err != nil {
			t.Fatalf("find %v: %v", tc.args, err)
		}
		if got := requiresExclusiveStorage(cmd); got != tc.want {
			t.Errorf("requiresExclusiveStorage(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}
