package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
)

func newTestPackage(repoName, pkgName, url string) *api.Package {
	return api.NewPackage().SetRepo(repoName).SetName(pkgName).SetGit(url)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func commitFile(t *testing.T, dir, name, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", msg)
	return gitOutput(t, dir, "rev-parse", "HEAD")
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestIsLocalGitURL(t *testing.T) {
	cases := map[string]bool{
		"/abs/path/repo":               true,
		"./relative/repo":              true,
		"relative/repo":                true,
		"file:///abs/path/repo":        true,
		"https://git.busybox.net/x":    false,
		"http://example.com/x.git":     false,
		"git@github.com:user/repo.git": false,
		"ssh://git@host/x.git":         false,
		"git://host/x.git":             false,
	}
	for url, want := range cases {
		if got := IsLocalGitURL(url); got != want {
			t.Errorf("IsLocalGitURL(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestEnsureSourceLocalURLShallowClone(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	commitFile(t, src, "lib.c", "int val = 2;\n", "v2")
	runGit(t, src, "tag", "v2.0.0")

	dep, cache := t.TempDir(), t.TempDir()
	m := NewSourceManager(dep, cache)
	req := SourceRequest{Key: "native/sample", URLs: []string{src}, Version: "1.0.0", Ref: "v1.0.0"}
	res, err := m.EnsureSource(req)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dep, "native", "sample", "src")
	if res.SrcDir != want {
		t.Fatalf("SrcDir = %q, want %q", res.SrcDir, want)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("materialized content = %q", data)
	}
	// Local path remotes go through the same shallow fetch, so the tree holds
	// only the requested snapshot.
	if shallow := gitOutput(t, res.SrcDir, "rev-parse", "--is-shallow-repository"); shallow != "true" {
		t.Fatalf("local clone is not shallow: %s", shallow)
	}
	if got := gitOutput(t, res.SrcDir, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("local clone kept history: %s commits", got)
	}
	if tags := gitOutput(t, res.SrcDir, "tag", "--list"); !strings.Contains(tags, "v1.0.0") || strings.Contains(tags, "v2.0.0") {
		t.Fatalf("tree tags = %q", tags)
	}

	// Same identity must reuse the tree, keeping local modifications.
	note := filepath.Join(res.SrcDir, "local-note")
	if err := os.WriteFile(note, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	res2, err := m.EnsureSource(req)
	if err != nil {
		t.Fatal(err)
	}
	if res2.SrcDir != res.SrcDir || res2.Commit != res.Commit {
		t.Fatalf("reuse changed result: %#v vs %#v", res2, res)
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("reuse re-created the tree: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("reused tree content = %q", data)
	}
}

func TestEnsureSourceRematerializesOnCommitChange(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	commitFile(t, src, "lib.c", "int val = 2;\n", "v2")
	runGit(t, src, "tag", "v2.0.0")

	m := NewSourceManager(t.TempDir(), t.TempDir())
	first, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(first.SrcDir, "local-note")
	if err := os.WriteFile(note, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v2.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if second.SrcDir != first.SrcDir {
		t.Fatalf("commit change moved the tree: %q vs %q", second.SrcDir, first.SrcDir)
	}
	if second.Commit == first.Commit {
		t.Fatal("commit change reused the old commit")
	}
	if _, err := os.Stat(note); !os.IsNotExist(err) {
		t.Fatalf("stale tree content survived re-materialization: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(second.SrcDir, "lib.c")); string(data) != "int val = 2;\n" {
		t.Fatalf("re-materialized content = %q", data)
	}
}

func TestEnsureSourceRecreatesOnPatchHashChange(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	m := NewSourceManager(t.TempDir(), t.TempDir())
	first, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0", PatchHash: "aaaa"})
	if err != nil {
		t.Fatal(err)
	}
	dirty := filepath.Join(first.SrcDir, "lib.c")
	if err := os.WriteFile(dirty, []byte("patched\n"), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0", PatchHash: "bbbb"})
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(second.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("patch change did not reset the tree: %q", data)
	}
	state, err := m.ReadState(second.Root)
	if err != nil || state == nil || state.PatchHash != "bbbb" {
		t.Fatalf("state = %#v, %v", state, err)
	}
}

func TestEnsureSourceURLChangeRematerializes(t *testing.T) {
	requireGit(t)
	srcA := writePatchableSource(t)
	srcB := writePatchableSource(t)
	commitB := commitFile(t, srcB, "lib.c", "int val = 2;\n", "v2")

	m := NewSourceManager(t.TempDir(), t.TempDir())
	first, err := m.EnsureSource(SourceRequest{Key: "local/sample", URLs: []string{srcA}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.EnsureSource(SourceRequest{Key: "local/sample", URLs: []string{srcB}})
	if err != nil {
		t.Fatal(err)
	}
	if second.Commit != commitB || second.Commit == first.Commit {
		t.Fatalf("URL change reused %s, want %s", second.Commit, commitB)
	}
	if data, _ := os.ReadFile(filepath.Join(second.SrcDir, "lib.c")); string(data) != "int val = 2;\n" {
		t.Fatalf("URL change kept stale content: %q", data)
	}
	if origin := gitOutput(t, second.SrcDir, "remote", "get-url", "origin"); origin != srcB {
		t.Fatalf("origin = %q, want %q", origin, srcB)
	}
}

func TestEnsureSourceMirrorMoveReclonesSameCommit(t *testing.T) {
	requireGit(t)
	srcA := writePatchableSource(t)
	parent := t.TempDir()
	runGit(t, parent, "clone", "--no-local", "-q", srcA, "mirror")
	srcB := filepath.Join(parent, "mirror")

	m := NewSourceManager(t.TempDir(), t.TempDir())
	first, err := m.EnsureSource(SourceRequest{Key: "local/sample", URLs: []string{srcA}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.EnsureSource(SourceRequest{Key: "local/sample", URLs: []string{srcB}})
	if err != nil {
		t.Fatal(err)
	}
	if second.Commit != first.Commit {
		t.Fatalf("same-commit mirror move changed commit: %s -> %s", first.Commit, second.Commit)
	}
	if origin := gitOutput(t, second.SrcDir, "remote", "get-url", "origin"); origin != srcB {
		t.Fatalf("tree kept the old origin: %q", origin)
	}
	state, err := m.ReadState(second.Root)
	if err != nil || state == nil || len(state.URLs) != 1 || state.URLs[0] != srcB {
		t.Fatalf("state = %#v, %v", state, err)
	}
}

func TestEnsureSourceWithoutURLsReusesMatchingCommit(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	m := NewSourceManager(t.TempDir(), t.TempDir())
	first, err := m.EnsureSource(SourceRequest{Key: "local/sample", URLs: []string{src}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.EnsureSource(SourceRequest{Key: "local/sample", Commit: first.Commit})
	if err != nil {
		t.Fatalf("URL-less reuse: %v", err)
	}
	if second.Commit != first.Commit || second.SrcDir != first.SrcDir {
		t.Fatalf("URL-less reuse = %s (%s), want %s (%s)", second.Commit, second.SrcDir, first.Commit, first.SrcDir)
	}
}

func TestEnsureSourceOfflineReuseAfterCachePurge(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	commitFile(t, src, "lib.c", "int val = 2;\n", "v2")
	runGit(t, src, "tag", "v2.0.0")

	dep, cache := t.TempDir(), t.TempDir()
	m := NewSourceManager(dep, cache)
	first, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(cache); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, src+"-away"); err != nil {
		t.Fatal(err)
	}

	offline := NewSourceManager(dep, cache)
	res, err := offline.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Commit: first.Commit})
	if err != nil {
		t.Fatalf("offline reuse: %v", err)
	}
	if res.Commit != first.Commit {
		t.Fatalf("offline commit = %q, want %q", res.Commit, first.Commit)
	}
	if _, err := offline.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v2.0.0"}); err == nil {
		t.Fatal("offline resolution without a source succeeded")
	}
	if _, err := os.Stat(filepath.Join(res.SrcDir, "lib.c")); err != nil {
		t.Fatalf("offline tree lost content: %v", err)
	}
}

func TestCleanSourceRemovesTree(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	m := NewSourceManager(t.TempDir(), t.TempDir())
	if _, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if err := m.CleanSource("native", "sample"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.TreeDir("native/sample")); !os.IsNotExist(err) {
		t.Fatalf("tree survived CleanSource: %v", err)
	}
}

func TestSourceManagerRejectsInvalidSessionReuse(t *testing.T) {
	requireGit(t)
	cache := t.TempDir()
	session, err := storage.Acquire("", cache, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	src := writePatchableSource(t)
	foreign := NewSourceManager(t.TempDir(), t.TempDir()).WithSession(session)
	if _, err := foreign.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Refresh: true}); err == nil {
		t.Fatal("manager accepted a session for a different cache")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	closed := NewSourceManager(t.TempDir(), cache).WithSession(session)
	if _, err := closed.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Refresh: true}); err == nil {
		t.Fatal("manager accepted a closed storage session")
	}
	if _, err := os.Stat(closed.TreeDir("native/sample")); !os.IsNotExist(err) {
		t.Fatalf("invalid session materialized a tree: %v", err)
	}
}

func TestEnsureSourceRefreshFollowsMovedRef(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	m := NewSourceManager(t.TempDir(), t.TempDir())
	first, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "master"})
	if err != nil {
		t.Fatal(err)
	}
	commit2 := commitFile(t, src, "lib.c", "int val = 2;\n", "v2")

	moved, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "master", Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Commit != commit2 || moved.Commit == first.Commit {
		t.Fatalf("refresh kept stale commit %s, want %s", moved.Commit, commit2)
	}
	if data, _ := os.ReadFile(filepath.Join(moved.SrcDir, "lib.c")); string(data) != "int val = 2;\n" {
		t.Fatalf("refreshed content = %q", data)
	}
	if _, err := os.Stat(filepath.Join(moved.SrcDir, "lib.c")); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureSourceRejectsEscapingKey(t *testing.T) {
	m := NewSourceManager(t.TempDir(), t.TempDir())
	for _, key := range []string{"../evil", "/abs", "a/../..", ""} {
		if _, err := m.EnsureSource(SourceRequest{Key: key, URLs: []string{"/nonexistent"}}); err == nil {
			t.Errorf("EnsureSource(%q) accepted an escaping key", key)
		}
		if err := m.CleanTree(key); err == nil {
			t.Errorf("CleanTree(%q) accepted an escaping key", key)
		}
	}
}

func TestEnsureSourceRecoversFromCorruptState(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	m := NewSourceManager(t.TempDir(), t.TempDir())
	req := SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0"}
	first, err := m.EnsureSource(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.Root, "state.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := m.EnsureSource(req)
	if err != nil {
		t.Fatalf("corrupt state was not recovered: %v", err)
	}
	state, err := m.ReadState(second.Root)
	if err != nil || state == nil || state.Commit == "" {
		t.Fatalf("state was not rewritten: %#v, %v", state, err)
	}
	if data, _ := os.ReadFile(filepath.Join(second.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("recovered content = %q", data)
	}
}

func TestEnsureSourceRepairsMismatchedHead(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	commitFile(t, src, "lib.c", "int val = 2;\n", "v2")
	runGit(t, src, "tag", "v2.0.0")
	m := NewSourceManager(t.TempDir(), t.TempDir())
	req := SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0"}
	first, err := m.EnsureSource(req)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash that left the tree at another commit while state still
	// records the old one. The shallow tree only knows v1.0.0, so fetch the
	// newer tag before moving HEAD.
	runGit(t, first.SrcDir, "fetch", "--depth", "1", "origin", "+refs/tags/v2.0.0:refs/tags/v2.0.0")
	runGit(t, first.SrcDir, "checkout", "--force", "--detach", "v2.0.0")
	second, err := m.EnsureSource(req)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(second.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("stale HEAD was not repaired: %q", data)
	}
}
