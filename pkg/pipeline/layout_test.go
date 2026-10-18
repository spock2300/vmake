package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/internal/storage"
)

func TestLayoutForLocalExactPaths(t *testing.T) {
	material := keyMaterial{commit: "abc", flagsHash: "flags"}
	layout, err := layoutFor(layoutInput{
		Local:     true,
		SourceDir: "/src/pkg",
		CCKey:     "/usr/bin/cc@1",
		Mode:      "debug",
		Options:   map[string]any{"x": 1},
		Material:  material,
		Ready:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantKey := "c966a9af47d2c1476cafdf245722587dfcf1331a0c6966b22f19c77a6aad68eb"
	if layout.BuildKey != wantKey {
		t.Fatalf("BuildKey = %q, want %q", layout.BuildKey, wantKey)
	}
	if layout.Dirs.SourceDir != "/src/pkg" {
		t.Errorf("SourceDir = %q", layout.Dirs.SourceDir)
	}
	if want := filepath.Join("/src/pkg", "build", wantKey); layout.Dirs.BuildDir != want {
		t.Errorf("BuildDir = %q, want %q", layout.Dirs.BuildDir, want)
	}
	if layout.Dirs.InstallDir != "" {
		t.Errorf("local InstallDir = %q, want empty", layout.Dirs.InstallDir)
	}
}

func TestLayoutForRemoteMemberExactPaths(t *testing.T) {
	material := keyMaterial{version: "1.0.0", commit: "commit", flagsHash: "flags", patchHash: "patch"}
	layout, err := layoutFor(layoutInput{
		PkgRoot:  "/deps/native/root",
		Member:   "nested/member",
		CCKey:    "/usr/bin/cc@1",
		Mode:     "release",
		Options:  map[string]any{"x": 1},
		Material: material,
		Ready:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantKey := "e05b58c4383404ec6def9f94704703cd6f00ca74b62ce10c16e42aaafd040fbb"
	output := filepath.Join("/deps/native/root", "out", storage.OwnerKey("nested/member"), wantKey)
	if want := filepath.Join("/deps/native/root", "src", "nested", "member"); layout.Dirs.SourceDir != want {
		t.Errorf("SourceDir = %q, want %q", layout.Dirs.SourceDir, want)
	}
	if want := filepath.Join(output, "build"); layout.Dirs.BuildDir != want {
		t.Errorf("BuildDir = %q, want %q", layout.Dirs.BuildDir, want)
	}
	if want := filepath.Join(output, "install"); layout.Dirs.InstallDir != want {
		t.Errorf("InstallDir = %q, want %q", layout.Dirs.InstallDir, want)
	}
}

func TestLayoutForPropagatesBuildKeyError(t *testing.T) {
	if _, err := layoutFor(layoutInput{
		Local:     true,
		SourceDir: "/src/pkg",
		CCKey:     "cc",
		Mode:      "debug",
		Options:   map[string]any{"bad": make(chan int)},
		Material:  keyMaterial{},
		Ready:     true,
	}); err == nil {
		t.Fatal("layoutFor accepted a non-JSON option value")
	}
}

func TestLayoutForUnknownLayoutKeepsSourceOnly(t *testing.T) {
	layout, err := layoutFor(layoutInput{
		Local:     true,
		SourceDir: "/src/pkg",
		CCKey:     "cc",
		Mode:      "debug",
		Material:  keyMaterial{},
		Ready:     false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if layout.Ready {
		t.Fatal("unknown layout marked ready")
	}
	if layout.Dirs.SourceDir != "/src/pkg" {
		t.Errorf("SourceDir = %q", layout.Dirs.SourceDir)
	}
}
