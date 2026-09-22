package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSymlinkRepairsWrongWindowsKind(t *testing.T) {
	if !SymlinksSupported() {
		t.Skip(SymlinkHint)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSymlink(link, target); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(link); err != nil || !info.IsDir() {
		t.Fatalf("file link was not repaired to directory link: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSymlink(link, target); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(link); err != nil || string(data) != "file" {
		t.Fatalf("directory link was not repaired to file link: %q, %v", data, err)
	}
}
