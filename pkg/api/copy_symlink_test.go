package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/fs"
)

func TestCopyDirFollowsSymlinks(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	if err := os.MkdirAll(filepath.Join(src, "real"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "real", "data"), []byte("payload"), 0644); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		filepath.Join(dir, "root-link"): src,
		filepath.Join(src, "dir-link"):  filepath.Join(src, "real"),
		filepath.Join(src, "file-link"): filepath.Join(src, "real", "data"),
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(link) })
	}
	dest := filepath.Join(dir, "dest")
	if err := CopyDir(filepath.Join(dir, "root-link"), dest); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"real/data", "dir-link/data", "file-link"} {
		if data, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(file))); err != nil || string(data) != "payload" {
			t.Fatalf("copied %s = %q, %v", file, data, err)
		}
	}
	for _, name := range []string{"dir-link", "file-link"} {
		if info, err := os.Lstat(filepath.Join(dest, name)); err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("copied %s should be a real entity: %v", name, err)
		}
	}
	filtered := filepath.Join(dir, "filtered")
	if err := CopyDirWithFilter(src, filtered, func(path string, isDir bool) bool {
		if filepath.Base(path) == "dir-link" {
			if !isDir {
				t.Error("directory symlink passed to filter as file")
			}
			return false
		}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filtered, "dir-link")); !os.IsNotExist(err) {
		t.Fatalf("directory link was not filtered: %v", err)
	}
}

func TestCopyDirRejectsCycles(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	src := t.TempDir()
	cycle := filepath.Join(src, "cycle")
	if err := os.Symlink(src, cycle); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(cycle) })
	if err := CopyDir(src, t.TempDir()); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestCopyDirRejectsDestinationInsideSource(t *testing.T) {
	for _, child := range []string{".", "child"} {
		src := t.TempDir()
		if err := CopyDir(src, filepath.Join(src, child)); err == nil || !strings.Contains(err.Error(), "inside source") {
			t.Fatalf("destination %s error = %v", child, err)
		}
	}
}

func TestCopyDirRejectsLinkToDestination(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	src := t.TempDir()
	dest := t.TempDir()
	link := filepath.Join(src, "output")
	if err := os.Symlink(dest, link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	if err := CopyDir(src, dest); err == nil || !strings.Contains(err.Error(), "reaches destination") {
		t.Fatalf("destination link error = %v", err)
	}
}

func TestCopyDirRejectsLinkToDestinationSubdirectory(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	for _, subdir := range []string{"shared", "nested/shared"} {
		t.Run(subdir, func(t *testing.T) {
			src := t.TempDir()
			dest := t.TempDir()
			target := filepath.Join(dest, filepath.FromSlash(subdir))
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Fatal(err)
			}
			dataPath := filepath.Join(target, "data")
			if err := os.WriteFile(dataPath, []byte("payload"), 0644); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(src, "shared")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Remove(link) })
			if err := CopyDir(src, dest); err == nil || !strings.Contains(err.Error(), "reaches destination") {
				t.Fatalf("destination subdirectory link error = %v", err)
			}
			if data, err := os.ReadFile(dataPath); err != nil || string(data) != "payload" {
				t.Fatalf("original data = %q, %v", data, err)
			}
			if err := CopyDirWithFilter(src, dest, func(path string, isDir bool) bool {
				return path != link
			}); err != nil {
				t.Fatalf("filtered destination subdirectory link: %v", err)
			}
		})
	}
}

func TestCopyDirFiltersBrokenSymlink(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "keep.c"), []byte("payload"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(src, "unused.h")
	if err := os.Symlink(filepath.Join(src, "missing.h"), link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	for _, mode := range []string{"excluded", "included", "unfiltered"} {
		t.Run(mode, func(t *testing.T) {
			var filter CopyFilter
			calls := 0
			if mode != "unfiltered" {
				filter = func(path string, isDir bool) bool {
					if path == link {
						calls++
						if isDir {
							t.Error("broken symlink passed to filter as directory")
						}
						return mode == "included"
					}
					return true
				}
			}
			dest := t.TempDir()
			err := CopyDirWithFilter(src, dest, filter)
			if mode == "excluded" {
				if err != nil {
					t.Fatal(err)
				}
				if data, err := os.ReadFile(filepath.Join(dest, "keep.c")); err != nil || string(data) != "payload" {
					t.Fatalf("copied data = %q, %v", data, err)
				}
				if _, err := os.Lstat(filepath.Join(dest, "unused.h")); !os.IsNotExist(err) {
					t.Fatalf("excluded symlink exists: %v", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("broken symlink error = %v", err)
			}
			if mode != "unfiltered" && calls != 1 {
				t.Errorf("filter calls for broken symlink = %d, want 1", calls)
			}
		})
	}
}

func TestCopyFileRejectsSameFile(t *testing.T) {
	for _, alias := range []string{"same path", "hard link", "source symlink", "destination symlink"} {
		t.Run(alias, func(t *testing.T) {
			if strings.Contains(alias, "symlink") && !fs.SymlinksSupported() {
				t.Skip(fs.SymlinkHint)
			}
			dir := t.TempDir()
			original := filepath.Join(dir, "data")
			if err := os.WriteFile(original, []byte("payload"), 0644); err != nil {
				t.Fatal(err)
			}
			src, dest := original, filepath.Join(dir, "alias")
			switch alias {
			case "same path":
				dest = original
			case "hard link":
				if err := os.Link(original, dest); err != nil {
					t.Fatal(err)
				}
			case "source symlink", "destination symlink":
				if err := os.Symlink(original, dest); err != nil {
					t.Fatal(err)
				}
				link := dest
				t.Cleanup(func() { _ = os.Remove(link) })
				if alias == "source symlink" {
					src, dest = dest, original
				}
			}
			if err := CopyFile(src, dest); err == nil || !strings.Contains(err.Error(), "same file") {
				t.Errorf("same-file copy error = %v", err)
			}
			if data, err := os.ReadFile(original); err != nil || string(data) != "payload" {
				t.Fatalf("original data = %q, %v", data, err)
			}
		})
	}
}

func TestCopyFileTruncatesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	dest := filepath.Join(dir, "dest")
	if err := os.WriteFile(src, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old trailing contents"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dest); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(dest); err != nil || string(data) != "new" {
		t.Fatalf("copied data = %q, %v", data, err)
	}
}
