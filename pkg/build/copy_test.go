package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spock2300/vmake/pkg/api"
)

func TestNativeCopyFilePreservesUnchangedTimestampAndRepairsDestination(t *testing.T) {
	dir := t.TempDir()
	src, dest := filepath.Join(dir, "source.h"), filepath.Join(dir, "installed.h")
	if err := os.WriteFile(src, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dest); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(dest, old, old); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dest); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(dest)
	if err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("unchanged copy changed mtime: %v", err)
	}
	if err := os.WriteFile(dest, []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dest, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	for _, remove := range []bool{false, true} {
		if remove {
			if err := os.Remove(dest); err != nil {
				t.Fatal(err)
			}
		}
		if err := CopyFile(src, dest); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(dest); err != nil || string(data) != "original" {
			t.Fatalf("destination was not repaired: %q, %v", data, err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(src, 0750); err != nil {
			t.Fatal(err)
		}
		if err := CopyFile(src, dest); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(dest); err != nil || info.Mode().Perm() != 0750 {
			t.Fatalf("destination mode was not repaired: %v, %v", info, err)
		}
	}
	if err := os.Chtimes(dest, old, old); err != nil {
		t.Fatal(err)
	}
	if err := api.CopyFile(src, dest); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dest); err != nil || info.ModTime().Before(old.Add(time.Minute)) {
		t.Fatalf("script CopyFile stopped rewriting files: %v, %v", info, err)
	}
	if err := CopyFile(src, src); err == nil {
		t.Fatal("same-file copy was accepted")
	}
}

func TestNativeFilteredCopyPreservesUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	src, dest := filepath.Join(dir, "source"), filepath.Join(dir, "installed")
	for _, name := range []string{"nested/value.h", "nested/ignore.c", "private/hidden.h"} {
		writeAssemblyFixture(t, src, name, name)
	}
	filter := func(path string, isDir bool) bool {
		if isDir {
			return filepath.Base(path) != "private"
		}
		return strings.HasSuffix(path, ".h")
	}
	if err := CopyDirWithFilter(src, dest, filter); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(dest, "nested", "value.h")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(installed, old, old); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	writeAssemblyFixture(t, src, "nested/new.h", "new")
	if err := CopyDirWithFilter(src, dest, filter); err != nil {
		t.Fatal(err)
	}
	if after, err := os.Stat(installed); err != nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("unchanged filtered header timestamp changed: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "nested", "new.h")); err != nil || string(data) != "new" {
		t.Fatalf("new matching header was not copied: %q, %v", data, err)
	}
	for _, name := range []string{"nested/ignore.c", "private/hidden.h"} {
		if _, err := os.Stat(filepath.Join(dest, name)); !os.IsNotExist(err) {
			t.Fatalf("filtered path %s was copied: %v", name, err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(src, "nested", "value.h"), 0750); err != nil {
			t.Fatal(err)
		}
		if err := CopyDirWithFilter(src, dest, filter); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(installed); err != nil || info.Mode().Perm() != 0750 {
			t.Fatalf("filtered header mode was not repaired: %v, %v", info, err)
		}
	}
	if err := CopyDir(src, filepath.Join(src, "nested", "copy")); err == nil {
		t.Fatal("copy destination inside source was accepted")
	}
}
