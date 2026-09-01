package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func writePatchableSource(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	runGit(t, src, "init", "-q", "-b", "master")
	runGit(t, src, "config", "user.email", "test@example.com")
	runGit(t, src, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(src, "lib.c"), []byte("int val = 1;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "add", "-A")
	runGit(t, src, "commit", "-q", "-m", "v1")
	return src
}

func patchedPkgRef(t *testing.T, patches ...string) *api.Package {
	t.Helper()
	scriptDir := t.TempDir()
	pkg := api.NewPackage().SetRepo("test").SetName("patched")
	pkg.SetScriptDir(scriptDir)
	for i, p := range patches {
		content := patches[i]
		if err := os.WriteFile(filepath.Join(scriptDir, p), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	pkg.SetPatches(patches...)
	return pkg
}

func TestPatchSetHashStableAndDistinct(t *testing.T) {
	p1 := patchedPkgRef(t, "a.patch")
	p1b := patchedPkgRef(t, "a.patch")
	p2 := patchedPkgRef(t, "a.patch", "b.patch")

	h1, err := PatchSetHash(p1)
	if err != nil {
		t.Fatal(err)
	}
	h1b, err := PatchSetHash(p1b)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := PatchSetHash(p2)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == "" || h1 != h1b {
		t.Errorf("same patch set should hash identically: %q vs %q", h1, h1b)
	}
	if h1 == h2 {
		t.Error("different patch sets should hash differently")
	}

	ab := patchedPkgRef(t, "a.patch", "b.patch")
	ba := patchedPkgRef(t, "b.patch", "a.patch")
	hab, err := PatchSetHash(ab)
	if err != nil {
		t.Fatal(err)
	}
	hba, err := PatchSetHash(ba)
	if err != nil {
		t.Fatal(err)
	}
	if hab == hba {
		t.Error("same patches in different declaration order must hash differently: patches form a series and do not commute")
	}

	none := api.NewPackage().SetRepo("test").SetName("nopatch")
	h, err := PatchSetHash(none)
	if err != nil || h != "" {
		t.Errorf("no patches should yield empty hash, got %q err=%v", h, err)
	}
}

func TestEnsurePatchedContentAddressed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	src := writePatchableSource(t)
	versionDir := t.TempDir()
	if err := os.Rename(src, filepath.Join(versionDir, "src")); err != nil {
		t.Fatal(err)
	}

	patchBody := "--- a/lib.c\n+++ b/lib.c\n@@ -1 +1 @@\n-int val = 1;\n+int val = 2;\n"
	pkgA := patchedPkgRef(t, "a.patch")
	if err := os.WriteFile(filepath.Join(pkgA.ScriptDir(), "a.patch"), []byte(patchBody), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewSourceManager(t.TempDir(), t.TempDir())

	patched1, err := m.EnsurePatched(pkgA, versionDir)
	if err != nil {
		t.Fatalf("first EnsurePatched: %v", err)
	}
	if patched1 == filepath.Join(versionDir, "src") {
		t.Fatal("patched package should not use the immutable src dir")
	}
	got, err := os.ReadFile(filepath.Join(patched1, "lib.c"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "int val = 2;\n" {
		t.Errorf("patched content = %q, want val 2", got)
	}
	orig, err := os.ReadFile(filepath.Join(versionDir, "src", "lib.c"))
	if err != nil {
		t.Fatal(err)
	}
	if string(orig) != "int val = 1;\n" {
		t.Errorf("immutable src was modified: %q", orig)
	}

	patched2, err := m.EnsurePatched(pkgA, versionDir)
	if err != nil {
		t.Fatalf("second EnsurePatched: %v", err)
	}
	if patched1 != patched2 {
		t.Errorf("identical patch set should reuse dir: %q vs %q", patched1, patched2)
	}

	pkgB := patchedPkgRef(t, "a.patch")
	if err := os.WriteFile(filepath.Join(pkgB.ScriptDir(), "a.patch"), []byte("--- a/lib.c\n+++ b/lib.c\n@@ -1 +1 @@\n-int val = 1;\n+int val = 3;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	patched3, err := m.EnsurePatched(pkgB, versionDir)
	if err != nil {
		t.Fatalf("EnsurePatched with different patch: %v", err)
	}
	if patched3 == patched1 {
		t.Error("different patch set must not share the patched dir")
	}
	got3, _ := os.ReadFile(filepath.Join(patched3, "lib.c"))
	if string(got3) != "int val = 3;\n" {
		t.Errorf("patched3 content = %q, want val 3", got3)
	}
}
