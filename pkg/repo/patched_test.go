package repo

import (
	"os"
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
