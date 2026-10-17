package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/internal/storage"
)

func TestLayoutForLocalExactPaths(t *testing.T) {
	material := keyMaterial{commit: "abc", flagsHash: "flags", scriptHash: "script"}
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
	wantKey := "42488ebef6ed1090f31a395f2e069141c310282282b604fdc66fd1092de99e59"
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
	material := keyMaterial{version: "1.0.0", commit: "commit", flagsHash: "flags", patchHash: "patch", scriptHash: "script"}
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
	wantKey := "1c29027a038a09b90f237a1a12d6baf105506d900e6b5d882f304ef0375b4c15"
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
