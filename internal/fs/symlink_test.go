package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureSymlinkPreservesEntities(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target")
			if err := os.Mkdir(target, 0755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "occupied")
			dataPath := link
			if directory {
				if err := os.Mkdir(link, 0755); err != nil {
					t.Fatal(err)
				}
				dataPath = filepath.Join(link, "keep")
			}
			if err := os.WriteFile(dataPath, []byte("keep"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := EnsureSymlink(link, target); err == nil || !strings.Contains(err.Error(), "not a symbolic link") {
				t.Fatalf("EnsureSymlink error = %v", err)
			}
			if data, err := os.ReadFile(dataPath); err != nil || string(data) != "keep" {
				t.Fatalf("existing data = %q, %v", data, err)
			}
		})
	}
}

func TestEnsureSymlinkReplacesOnlyLink(t *testing.T) {
	if !SymlinksSupported() {
		t.Skip(SymlinkHint)
	}
	dir := t.TempDir()
	for _, name := range []string{"old", "new"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "keep"), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "link")
	if err := EnsureSymlink(link, "old"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSymlink(link, "new"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSymlink(link, "new"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(link, "keep")); err != nil || string(data) != "new" {
		t.Fatalf("linked data = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "old", "keep")); err != nil || string(data) != "old" {
		t.Fatalf("old target data = %q, %v", data, err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link is not a symbolic link: %v", err)
	}
}

func TestEnsureSymlinkRejectsMissingTarget(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := EnsureSymlink(link, "missing"); !os.IsNotExist(err) && (err == nil || !strings.Contains(err.Error(), "stat symlink target")) {
		t.Fatalf("EnsureSymlink error = %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("missing-target link was created: %v", err)
	}
}
