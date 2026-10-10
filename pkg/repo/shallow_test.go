package repo

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fileURL(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
}

func TestEnsureSourceShallowTagClone(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	commitFile(t, src, "lib.c", "int val = 2;\n", "v2")
	runGit(t, src, "tag", "v2.0.0")

	m := NewSourceManager(t.TempDir(), t.TempDir())
	res, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{fileURL(t, src)}, Version: "1.0.0", Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, res.SrcDir, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("network clone is not shallow: %s", got)
	}
	if got := gitOutput(t, res.SrcDir, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("clone kept history: %s commits", got)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("materialized content = %q", data)
	}
	if tag := gitOutput(t, res.SrcDir, "describe", "--tags", "--exact-match", "HEAD"); tag != "v1.0.0" {
		t.Fatalf("tree lost its version tag: %q", tag)
	}
	// Other tags and their commits must not have been downloaded.
	probe := exec.Command("git", "-C", res.SrcDir, "rev-parse", "v2.0.0^{commit}")
	if out, err := probe.CombinedOutput(); err == nil {
		t.Fatalf("shallow clone contains newer tag: %s", out)
	}
}

func TestEnsureSourceFloatingClonesDefaultBranch(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	head := commitFile(t, src, "lib.c", "int val = 2;\n", "head move")

	m := NewSourceManager(t.TempDir(), t.TempDir())
	res, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{fileURL(t, src)}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Commit != head {
		t.Fatalf("commit = %s, want %s", res.Commit, head)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 2;\n" {
		t.Fatalf("materialized content = %q", data)
	}
	if got := gitOutput(t, res.SrcDir, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("floating clone is not shallow: %s", got)
	}
}

func TestEnsureSourceCommitOnlyClonesBySHA(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	old := gitOutput(t, src, "rev-parse", "HEAD")
	commitFile(t, src, "lib.c", "int val = 2;\n", "v2")

	m := NewSourceManager(t.TempDir(), t.TempDir())
	res, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{fileURL(t, src)}, Ref: old})
	if err != nil {
		t.Fatal(err)
	}
	if res.Commit != old {
		t.Fatalf("commit = %s, want %s", res.Commit, old)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("materialized content = %q", data)
	}
	if got := gitOutput(t, res.SrcDir, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("commit clone is not shallow: %s", got)
	}
}

func TestEnsureSourceMultiURLFallback(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	missing := filepath.Join(t.TempDir(), "missing.git")
	good := fileURL(t, src)

	m := NewSourceManager(t.TempDir(), t.TempDir())
	res, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{missing, good}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != good {
		t.Fatalf("used URL = %q, want %q", res.URL, good)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("materialized content = %q", data)
	}
}

func TestListRemoteTags(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	runGit(t, src, "tag", "-a", "v2.0.0", "-m", "annotated")

	tags, err := ListRemoteTags([]string{fileURL(t, src)})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(tags, ",")
	if !strings.Contains(got, "v1.0.0") || !strings.Contains(got, "v2.0.0") {
		t.Fatalf("tags = %v", tags)
	}
	for _, tag := range tags {
		if strings.Contains(tag, "^{}") {
			t.Fatalf("peeled tag leaked into tag list: %v", tags)
		}
	}

	// The first URL failing must fall back to the next one.
	missing := filepath.Join(t.TempDir(), "missing.git")
	tags, err = ListRemoteTags([]string{missing, fileURL(t, src)})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Fatalf("fallback tags = %v", tags)
	}
}

func TestEnsureSourceFullyQualifiedRefs(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	runGit(t, src, "tag", "-a", "v2.0.0", "-m", "annotated")

	m := NewSourceManager(t.TempDir(), t.TempDir())
	tag, err := m.EnsureSource(SourceRequest{Key: "native/tag", URLs: []string{fileURL(t, src)}, Ref: "refs/tags/v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if want := gitOutput(t, src, "rev-parse", "v1.0.0^{commit}"); tag.Commit != want {
		t.Fatalf("fully qualified tag commit = %s, want %s", tag.Commit, want)
	}

	annotated, err := m.EnsureSource(SourceRequest{Key: "native/annotated", URLs: []string{fileURL(t, src)}, Ref: "refs/tags/v2.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if want := gitOutput(t, src, "rev-parse", "v2.0.0^{commit}"); annotated.Commit != want {
		t.Fatalf("fully qualified annotated tag commit = %s, want %s", annotated.Commit, want)
	}
	if got := gitOutput(t, annotated.SrcDir, "describe", "--tags", "--exact-match", "HEAD"); got != "v2.0.0" {
		t.Fatalf("annotated tag lost in tree: %q", got)
	}

	branch, err := m.EnsureSource(SourceRequest{Key: "native/branch", URLs: []string{fileURL(t, src)}, Ref: "refs/heads/master"})
	if err != nil {
		t.Fatal(err)
	}
	if want := gitOutput(t, src, "rev-parse", "HEAD"); branch.Commit != want {
		t.Fatalf("fully qualified branch commit = %s, want %s", branch.Commit, want)
	}

	head, err := m.EnsureSource(SourceRequest{Key: "native/head", URLs: []string{fileURL(t, src)}, Ref: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if want := gitOutput(t, src, "rev-parse", "HEAD"); head.Commit != want {
		t.Fatalf("HEAD commit = %s, want %s", head.Commit, want)
	}
}

func TestEnsureSourceRestoresPatchChangeOffline(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	m := NewSourceManager(t.TempDir(), t.TempDir())
	first, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0", PatchHash: "aaaa"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an applied patch and a stray file, then make the source
	// unreachable: the same commit must be restorable without the network.
	if err := os.WriteFile(filepath.Join(first.SrcDir, "lib.c"), []byte("patched\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.SrcDir, "extra.txt"), []byte("stray"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, src+"-away"); err != nil {
		t.Fatal(err)
	}

	second, err := m.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0", PatchHash: "bbbb"})
	if err != nil {
		t.Fatalf("offline patch restore: %v", err)
	}
	if second.Commit != first.Commit {
		t.Fatalf("commit changed: %s -> %s", first.Commit, second.Commit)
	}
	if data, _ := os.ReadFile(filepath.Join(second.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("patch change did not reset the tree: %q", data)
	}
	if _, err := os.Stat(filepath.Join(second.SrcDir, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("stray file survived restore: %v", err)
	}
	state, err := m.ReadState(second.Root)
	if err != nil || state == nil || state.PatchHash != "bbbb" {
		t.Fatalf("state = %#v, %v", state, err)
	}
}
