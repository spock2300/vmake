package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/internal/storage"
)

func TestLayoutForLocalExactPaths(t *testing.T) {
	material := LayoutFacts{Commit: "abc", FlagsHash: "flags"}
	layout, err := layoutFor(layoutInput{
		Local:     true,
		SourceDir: "/src/pkg",
		CCKey:     "/usr/bin/cc@1",
		Mode:      "debug",
		Options:   map[string]any{"x": 1},
		Facts:     material,
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
	material := LayoutFacts{Version: "1.0.0", Commit: "commit", FlagsHash: "flags", PatchHash: "patch"}
	layout, err := layoutFor(layoutInput{
		PkgRoot: "/deps/native/root",
		Member:  "nested/member",
		CCKey:   "/usr/bin/cc@1",
		Mode:    "release",
		Options: map[string]any{"x": 1},
		Facts:   material,
		Ready:   true,
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
		Facts:     LayoutFacts{},
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
		Facts:     LayoutFacts{},
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

func layoutKeyFor(t *testing.T, in layoutInput) string {
	t.Helper()
	layout, err := layoutFor(in)
	if err != nil {
		t.Fatal(err)
	}
	return layout.BuildKey
}

// LayoutFacts is the whole key material: every field a package kind uses must
// change its build key, so neither adapter can drop one silently.
func TestLayoutFactsCarryAllKeyMaterial(t *testing.T) {
	local := layoutInput{
		Local:     true,
		SourceDir: "/src/app",
		CCKey:     "cc",
		Mode:      "debug",
		Facts:     LayoutFacts{Commit: "c1", FlagsHash: "f1"},
		Ready:     true,
	}
	base := layoutKeyFor(t, local)
	for name, mutate := range map[string]func(*LayoutFacts){
		"commit": func(f *LayoutFacts) { f.Commit = "c2" },
		"flags":  func(f *LayoutFacts) { f.FlagsHash = "f2" },
	} {
		in := local
		mutate(&in.Facts)
		if got := layoutKeyFor(t, in); got == base {
			t.Fatalf("local %s did not change the build key", name)
		}
	}

	remote := layoutInput{
		PkgRoot: "/deps/owner",
		Member:  "member",
		CCKey:   "cc",
		Mode:    "debug",
		Facts: LayoutFacts{
			Version: "1.0.0", Commit: "c1", FlagsHash: "f1", PatchHash: "p1", SourceLink: "/link",
		},
		Ready: true,
	}
	base = layoutKeyFor(t, remote)
	for name, mutate := range map[string]func(*LayoutFacts){
		"version": func(f *LayoutFacts) { f.Version = "2.0.0" },
		"commit":  func(f *LayoutFacts) { f.Commit = "c2" },
		"flags":   func(f *LayoutFacts) { f.FlagsHash = "f2" },
		"patch":   func(f *LayoutFacts) { f.PatchHash = "p2" },
		"link":    func(f *LayoutFacts) { f.SourceLink = "/other" },
	} {
		in := remote
		mutate(&in.Facts)
		if got := layoutKeyFor(t, in); got == base {
			t.Fatalf("remote %s did not change the build key", name)
		}
	}
}
