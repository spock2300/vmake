package pipeline

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLayoutForLinkedSourceOverride(t *testing.T) {
	root := filepath.Join(t.TempDir(), "deps", "hl", "sdk")
	link := filepath.Join(t.TempDir(), "sdk")
	layout, err := layoutFor(layoutInput{
		PkgRoot:        root,
		SourceOverride: link,
		Member:         "system/freertos/lwip",
		CCKey:          "cc",
		Mode:           "debug",
		Material:       keyMaterial{version: "1.0.0", commit: "c"},
		Ready:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if layout.Dirs.SourceDir != link {
		t.Fatalf("SourceDir = %q, want %q", layout.Dirs.SourceDir, link)
	}
	if !isWithin(filepath.Join(root, "out"), layout.Dirs.BuildDir) {
		t.Fatalf("BuildDir = %q, want under %s", layout.Dirs.BuildDir, filepath.Join(root, "out"))
	}
}

func TestLayoutForLinkedSourceOverrideMemberIsFullPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "deps", "hl", "sdk")
	link := filepath.Join(t.TempDir(), "sdk", "member")
	layout, err := layoutFor(layoutInput{
		PkgRoot:        root,
		SourceOverride: link,
		Member:         "member",
		CCKey:          "cc",
		Mode:           "debug",
		Material:       keyMaterial{version: "1.0.0", commit: "c"},
		Ready:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if layout.Dirs.SourceDir != link {
		t.Fatalf("SourceDir = %q, want %q", layout.Dirs.SourceDir, link)
	}
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
