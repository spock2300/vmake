package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
)

func TestWorkspaceIsolatedAndIncremental(t *testing.T) {
	seed := writePatchableSource(t)
	if err := os.Mkdir(filepath.Join(seed, "zdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "zdir", "nested"), []byte("directory link"), 0644); err != nil {
		t.Fatal(err)
	}
	if fs.SymlinksSupported() {
		if err := os.Symlink("lib.c", filepath.Join(seed, "alias.c")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("zdir", filepath.Join(seed, "a-directory")); err != nil {
			t.Fatal(err)
		}
	}
	m := NewSourceManager(t.TempDir(), t.TempDir())
	a, b := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	for _, dest := range []string{a, b} {
		if err := m.EnsureWorkspace(seed, dest, "source-1"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a, "lib.c"), []byte("private change"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{seed, b} {
		data, err := os.ReadFile(filepath.Join(dir, "lib.c"))
		if err != nil || string(data) != "int val = 1;\n" {
			t.Fatalf("source leaked between workspaces: %s = %q, %v", dir, data, err)
		}
	}
	if err := m.EnsureWorkspace(seed, a, "source-1"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(a, "lib.c")); string(data) != "private change" {
		t.Fatal("unchanged source identity recreated workspace")
	}
	if err := m.EnsureWorkspace(seed, a, "source-2"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(a, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatal("changed source identity did not reset workspace")
	}
	if fs.SymlinksSupported() {
		if info, err := os.Lstat(filepath.Join(a, "alias.c")); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("workspace source symlink was not preserved: %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(a, "a-directory", "nested")); err != nil || string(data) != "directory link" {
			t.Fatalf("directory symlink lost its kind: %q, %v", data, err)
		}
	}
}

func TestCanonicalPathUnifiesSymlinkSpellings(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(filepath.Join(real, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	want := canonicalPath(filepath.Join(real, "sub", "work"))
	for _, spelling := range []string{
		filepath.Join(real, "sub", "work"),
		filepath.Join(link, "sub", "work"),
		filepath.Join(link, "sub", "..", "sub", "work"),
	} {
		if got := canonicalPath(spelling); got != want {
			t.Errorf("canonicalPath(%s) = %s, want %s", spelling, got, want)
		}
	}
}

func TestProjectVersionResolvesSymlinkedCacheRoot(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	root := t.TempDir()
	realCache := filepath.Join(root, "real-cache")
	if err := os.MkdirAll(realCache, 0755); err != nil {
		t.Fatal(err)
	}
	linkCache := filepath.Join(root, "cache-link")
	if err := os.Symlink(realCache, linkCache); err != nil {
		t.Fatal(err)
	}
	m := NewSourceManager(t.TempDir(), linkCache)
	pkg := api.NewPackage().SetRepo("native").SetName("sample")
	versionDir := m.versionDir(pkg, "1.0.0")
	if err := os.MkdirAll(filepath.Join(versionDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := m.linkProject(pkg, versionDir); err != nil {
		t.Fatal(err)
	}
	version, err := m.ProjectVersion("native", "sample")
	if err != nil {
		t.Fatalf("ProjectVersion: %v", err)
	}
	if version != "1.0.0" {
		t.Fatalf("version = %q, want 1.0.0", version)
	}
}

func TestCleanOutputsDeletesSharedArtifacts(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	m := NewSourceManager(t.TempDir(), t.TempDir())
	pkg := api.NewPackage().SetRepo("native").SetName("sample")
	versionDir := m.versionDir(pkg, "1.0.0")
	if err := os.MkdirAll(filepath.Join(versionDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := m.linkProject(pkg, versionDir); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(versionDir, "out", "artifact.a")
	if err := os.WriteFile(artifact, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.CleanOutputs("native", "sample"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("shared output survived cleanup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(versionDir, "src")); err != nil {
		t.Fatal("output cleanup deleted source")
	}
	if err := m.linkProject(pkg, versionDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatal("relink restored deleted output")
	}
}

func TestCleanupRejectsSharedSession(t *testing.T) {
	cache := t.TempDir()
	s, err := storage.Acquire("", cache, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := NewSourceManager(t.TempDir(), cache).WithSession(s)
	if err := m.CleanVersion("native", "sample", "1.0.0"); err == nil {
		t.Fatal("clean upgraded a live build session")
	}
}

func TestProjectVersionRejectsForeignSourceLink(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	m := NewSourceManager(t.TempDir(), t.TempDir())
	pkg := api.NewPackage().SetRepo("native").SetName("sample")
	if err := fs.EnsureSymlink(m.localSrcPath(pkg), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ProjectVersion("native", "sample"); err == nil {
		t.Fatal("foreign source directory was accepted as a cache version")
	}
}

func TestSourceManagerRejectsInvalidSessionReuse(t *testing.T) {
	cache := t.TempDir()
	session, err := storage.Acquire("", cache, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	seed := writePatchableSource(t)
	work := filepath.Join(t.TempDir(), "work")
	foreign := NewSourceManager(t.TempDir(), t.TempDir()).WithSession(session)
	if err := foreign.EnsureWorkspace(seed, work, "source"); err == nil {
		t.Fatal("manager accepted a session for a different cache")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	closed := NewSourceManager(t.TempDir(), cache).WithSession(session)
	if err := closed.EnsureWorkspace(seed, work, "source"); err == nil {
		t.Fatal("manager accepted a closed storage session")
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Fatalf("invalid session materialized a workspace: %v", err)
	}
}

func TestWorkspacePatchesRefreshCopiedIndexWithoutStagingChanges(t *testing.T) {
	seed := writePatchableSource(t)
	if err := os.WriteFile(filepath.Join(seed, "unrelated.c"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", "unrelated.c")
	runGit(t, seed, "commit", "-q", "-m", "unrelated source")
	patch := filepath.Join(t.TempDir(), "change.patch")
	if err := os.WriteFile(patch, []byte("--- a/lib.c\n+++ b/lib.c\n@@ -1 +1 @@\n-int val = 1;\n+int val = 2;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	manager := NewSourceManager(t.TempDir(), t.TempDir())
	for _, changed := range []string{"", "unrelated.c", "lib.c"} {
		t.Run("changed="+changed, func(t *testing.T) {
			work := filepath.Join(t.TempDir(), "work")
			if err := manager.EnsureWorkspace(seed, work, "source"); err != nil {
				t.Fatal(err)
			}
			if changed != "" {
				if err := os.WriteFile(filepath.Join(work, changed), []byte("private changes\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			err := ApplyPatch(work, patch)
			if changed == "lib.c" {
				if err == nil {
					t.Fatal("patch overwrote a modified tracked source")
				}
			} else if err != nil {
				t.Fatalf("patch rejected a clean copied index entry: %v", err)
			}
			if changed != "" {
				data, err := os.ReadFile(filepath.Join(work, changed))
				if err != nil || string(data) != "private changes\n" {
					t.Fatalf("private source was changed: %q, %v", data, err)
				}
				runGit(t, work, "diff", "--cached", "--exit-code", "--", changed)
			}
		})
	}
	data, err := os.ReadFile(filepath.Join(seed, "lib.c"))
	if err != nil || string(data) != "int val = 1;\n" {
		t.Fatalf("patch changed seed source: %q, %v", data, err)
	}
}
