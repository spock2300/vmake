package plugin

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeTestZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range entries {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatalf("create entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write entry %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	writeTestZip(t, archive, map[string]string{
		"top.txt":            "top",
		"nested/deep/x.h":    "deep",
		"nested/empty/":      "",
		"nested/../flat.txt": "flat",
	})

	dest := filepath.Join(dir, "out")
	if err := ExtractToDir(archive, dest, "zip"); err != nil {
		t.Fatalf("ExtractToDir: %v", err)
	}

	for name, want := range map[string]string{
		"top.txt":         "top",
		"nested/deep/x.h": "deep",
		"flat.txt":        "flat",
	} {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	if info, err := os.Stat(filepath.Join(dest, "nested", "empty")); err != nil || !info.IsDir() {
		t.Errorf("directory entry nested/empty was not created (err=%v)", err)
	}
}

func TestExtractZipRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.zip")
	writeTestZip(t, archive, map[string]string{"../escaped.txt": "nope"})

	dest := filepath.Join(dir, "out")
	if err := ExtractToDir(archive, dest, "zip"); err == nil {
		t.Fatal("ExtractToDir must reject a zip entry escaping the destination")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); err == nil {
		t.Fatal("zip entry escaped the destination directory")
	}
}
