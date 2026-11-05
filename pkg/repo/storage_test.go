package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanMaterializedSourceKeepsOutputsAndThirdParty(t *testing.T) {
	depsDir := t.TempDir()
	manager := NewSourceManager(depsDir, t.TempDir())
	root := manager.TreeDir(PackageTreeKey("native", "sample"))

	for _, path := range []string{
		filepath.Join(root, "src", "build.go"),
		filepath.Join(root, "state.json"),
		filepath.Join(root, "out", "build", "artifact.a"),
		filepath.Join(root, "third_party", "member", "src", "upstream.c"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("content\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := manager.CleanMaterializedSource("native", "sample"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "src"),
		filepath.Join(root, "state.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists", path)
		}
	}
	for _, path := range []string{
		filepath.Join(root, "out", "build", "artifact.a"),
		filepath.Join(root, "third_party", "member", "src", "upstream.c"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s was removed: %v", path, err)
		}
	}
}
