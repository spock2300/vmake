package repo

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeCacheURL(t *testing.T) {
	cases := map[string]string{
		"https://GitHub.com/foo/bar.git":  "https://github.com/foo/bar",
		"https://github.com/foo/bar/":     "https://github.com/foo/bar",
		"https://github.com/foo/bar.git/": "https://github.com/foo/bar",
		"git@github.com:user/repo.git":    "git@github.com:user/repo",
		"git@GitHub.com:User/Repo.git":    "git@github.com:User/Repo",
		"file:///tmp/local/repo.git":      "file:///tmp/local/repo",
		"/tmp/local/repo.git":             "/tmp/local/repo",
		"ssh://git@Host:2222/x/y.git":     "ssh://git@host:2222/x/y",
		"ssh://Git@Host:2222/x/y.git":     "ssh://Git@host:2222/x/y",
	}
	for raw, want := range cases {
		if got := normalizeCacheURL(raw); got != want {
			t.Errorf("normalizeCacheURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestRepoFileURLIsFileScheme(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "repo.git")
	got := RepoFileURL(abs)
	if !strings.HasPrefix(got, "file://") {
		t.Fatalf("RepoFileURL = %q, want file:// URL", got)
	}
	if strings.Contains(got, "\\") {
		t.Fatalf("RepoFileURL kept backslashes: %q", got)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("RepoFileURL produced an invalid URL: %v", err)
	}
	if u.Scheme != "file" {
		t.Fatalf("RepoFileURL scheme = %q, want file", u.Scheme)
	}
	// A leading '/' keeps the path in the URL path; without it a Windows drive
	// letter becomes the authority ("file://C:/x") and git reads C: as a host.
	if u.Host != "" {
		t.Fatalf("RepoFileURL put %q in the authority, want an empty host", u.Host)
	}
	want := "/" + strings.TrimPrefix(filepath.ToSlash(abs), "/")
	if u.Path != want {
		t.Fatalf("RepoFileURL path = %q, want %q", u.Path, want)
	}
}

func TestObjectCacheDisabledByEnv(t *testing.T) {
	t.Setenv("VMAKE_GIT_CACHE", "off")
	if NewObjectCache(t.TempDir()).Enabled() {
		t.Fatal("VMAKE_GIT_CACHE=off did not disable the cache")
	}
	t.Setenv("VMAKE_GIT_CACHE", "")
	if !NewObjectCache(t.TempDir()).Enabled() {
		t.Fatal("cache disabled without VMAKE_GIT_CACHE=off")
	}
}

// TestObjectCacheOffSkipsCache checks VMAKE_GIT_CACHE=off keeps materialization
// on the direct network path and never populates <cacheDir>/git.
func TestObjectCacheOffSkipsCache(t *testing.T) {
	requireGit(t)
	t.Setenv("VMAKE_GIT_CACHE", "off")
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")

	cache := t.TempDir()
	res, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{fileURL(t, src)}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, "git")); !os.IsNotExist(err) {
		t.Fatalf("cache populated while disabled: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("content = %q", data)
	}
}

// TestObjectCacheSharedAcrossProjects materializes the same commit from two
// projects sharing one cacheDir; the second succeeds after the upstream is
// gone and produces identical content.
func TestObjectCacheSharedAcrossProjects(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	first, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, src+"-away"); err != nil {
		t.Fatal(err)
	}

	second, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0", Commit: first.Commit})
	if err != nil {
		t.Fatalf("second project after upstream removal: %v", err)
	}
	if second.Commit != first.Commit {
		t.Fatalf("second commit = %s, want %s", second.Commit, first.Commit)
	}
	if second.SrcDir == first.SrcDir {
		t.Fatal("second project reused the first project's tree")
	}
	firstData, _ := os.ReadFile(filepath.Join(first.SrcDir, "lib.c"))
	secondData, _ := os.ReadFile(filepath.Join(second.SrcDir, "lib.c"))
	if string(firstData) != "int val = 1;\n" || string(secondData) != string(firstData) {
		t.Fatalf("content mismatch: first=%q second=%q", firstData, secondData)
	}
	if got := gitOutput(t, second.SrcDir, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("cached tree is not shallow: %s", got)
	}
	if got := gitOutput(t, second.SrcDir, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("cached tree kept history: %s commits", got)
	}
}

// TestObjectCacheHitSkipsUpstreamFetch asserts a populated cache serves a
// lock-pinned commit with no upstream, and that the commit is anchored.
func TestObjectCacheHitSkipsUpstreamFetch(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	first, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	cacheRepo := NewObjectCache(cache).RepoDir(gitURL)
	if _, err := os.Stat(filepath.Join(cacheRepo, "objects")); err != nil {
		t.Fatalf("cache repo missing: %v", err)
	}
	if anchor := gitOutput(t, cacheRepo, "rev-parse", "refs/vmake/"+first.Commit); anchor != first.Commit {
		t.Fatalf("cache anchor = %q, want %q", anchor, first.Commit)
	}

	if err := os.Rename(src, src+"-away"); err != nil {
		t.Fatal(err)
	}
	res, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Commit: first.Commit})
	if err != nil {
		t.Fatalf("offline cache hit: %v", err)
	}
	if res.Commit != first.Commit {
		t.Fatalf("commit = %s, want %s", res.Commit, first.Commit)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("content = %q", data)
	}
}

// TestObjectCacheTagRefRoundTrip checks that a lock-pinned tag request served
// from the cache keeps the tag, stays shallow and holds a single commit.
func TestObjectCacheTagRefRoundTrip(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	first, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, src+"-away"); err != nil {
		t.Fatal(err)
	}

	res, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0", Commit: first.Commit})
	if err != nil {
		t.Fatalf("cached tag round trip: %v", err)
	}
	if tag := gitOutput(t, res.SrcDir, "describe", "--tags", "--exact-match", "HEAD"); tag != "v1.0.0" {
		t.Fatalf("cached tree lost its tag: %q", tag)
	}
	if got := gitOutput(t, res.SrcDir, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("cached tag tree is not shallow: %s", got)
	}
	if got := gitOutput(t, res.SrcDir, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("cached tag tree kept history: %s commits", got)
	}
}

// TestObjectCacheHasListRemove covers the query/inventory API over a populated
// cache, including removal of a namespace.
func TestObjectCacheHasListRemove(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	c := NewObjectCache(cache)
	if c.Has(gitURL, "0000000000000000000000000000000000000000") {
		t.Fatal("Has reported an uncached commit")
	}
	first, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !c.Has(gitURL, first.Commit) {
		t.Fatal("Has did not report the cached commit")
	}
	if c.Has("https://elsewhere.invalid/repo", first.Commit) {
		t.Fatal("Has reported a commit for an unknown URL")
	}

	entries, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].URL != gitURL || entries[0].Commits != 1 {
		t.Fatalf("List = %#v", entries)
	}

	if err := c.Remove(gitURL); err != nil {
		t.Fatal(err)
	}
	if c.Has(gitURL, first.Commit) {
		t.Fatal("Has reported a removed commit")
	}
	if entries, err = c.List(); err != nil || len(entries) != 0 {
		t.Fatalf("List after Remove = %#v, %v", entries, err)
	}
}

// TestObjectCacheCorruptEntryRecreated breaks the cached objects and then the
// metadata; the next materialization rebuilds the namespace instead of blocking.
func TestObjectCacheCorruptEntryRecreated(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	first, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	cacheRepo := NewObjectCache(cache).RepoDir(gitURL)

	// Objects gone while metadata stays valid: the anchor no longer resolves,
	// so the commit is re-fetched from the upstream.
	if err := os.RemoveAll(filepath.Join(cacheRepo, "objects")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(cacheRepo, "objects"), 0755); err != nil {
		t.Fatal(err)
	}
	res, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Commit: first.Commit})
	if err != nil {
		t.Fatalf("cache with lost objects was not rebuilt: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("rebuilt content = %q", data)
	}

	// Unreadable metadata: the namespace is treated as foreign and recreated.
	if err := os.WriteFile(filepath.Join(cacheRepo, "vmake-cache.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err = NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Commit: first.Commit})
	if err != nil {
		t.Fatalf("corrupt cache metadata was not rebuilt: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("rebuilt content = %q", data)
	}
	assertCacheMetaRewritten(t, cacheRepo, gitURL)
}

// TestObjectCacheURLMismatchRecreates tampers with the recorded URL; the next
// materialization treats the namespace as foreign and rebuilds it.
func TestObjectCacheURLMismatchRecreates(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	first, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(NewObjectCache(cache).RepoDir(gitURL), "vmake-cache.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["url"] = "https://elsewhere.invalid/repo"
	tampered, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, tampered, 0644); err != nil {
		t.Fatal(err)
	}

	res, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Commit: first.Commit})
	if err != nil {
		t.Fatalf("URL mismatch was not rebuilt: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("rebuilt content = %q", data)
	}
	assertCacheMetaRewritten(t, NewObjectCache(cache).RepoDir(gitURL), gitURL)
}

// TestObjectCacheRefMaterializationLeavesNoRemoteRefs checks that a tag-based
// materialization anchors refs/vmake/<commit> and refs/tags/<tag> without
// triggering the ref-tip fallback (which would leave refs/remotes/origin/*).
func TestObjectCacheRefMaterializationLeavesNoRemoteRefs(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	first, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	repoDir := NewObjectCache(cache).RepoDir(gitURL)
	if anchor := gitOutput(t, repoDir, "rev-parse", "refs/vmake/"+first.Commit); anchor != first.Commit {
		t.Fatalf("cache anchor = %q, want %q", anchor, first.Commit)
	}
	if tag := gitOutput(t, repoDir, "rev-parse", "refs/tags/v1.0.0^{commit}"); tag != first.Commit {
		t.Fatalf("cache tag = %q, want %q", tag, first.Commit)
	}
	if remote := gitOutput(t, repoDir, "for-each-ref", "--format=%(refname)", "refs/remotes/origin"); remote != "" {
		t.Fatalf("ref-tip fallback ran during a tag materialization: %q", remote)
	}

	// A commit-only materialization anchors refs/vmake/<commit> in its own
	// namespace. (Absence of refs/remotes/origin/* is asserted on the tag path
	// above, where no by-SHA refusal can apply.)
	commitCache := t.TempDir()
	commitRes, err := NewSourceManager(t.TempDir(), commitCache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: first.Commit})
	if err != nil {
		t.Fatal(err)
	}
	commitRepo := NewObjectCache(commitCache).RepoDir(gitURL)
	if anchor := gitOutput(t, commitRepo, "rev-parse", "refs/vmake/"+commitRes.Commit); anchor != commitRes.Commit {
		t.Fatalf("commit cache anchor = %q, want %q", anchor, commitRes.Commit)
	}
}

// TestObjectCacheEnsureRefRecordsLocalHit checks the local hit path of
// EnsureRef still records ref->commit: without it a lock-pinned request would
// keep probing ls-remote (up to VMAKE_GIT_TIMEOUT) even though the ref is
// already cached.
func TestObjectCacheEnsureRefRecordsLocalHit(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	first, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	c := NewObjectCache(cache)
	repoDir := c.RepoDir(gitURL)

	// Forget the recorded mapping so the next call must take the local hit
	// path; the ref object is still in the bare repository.
	meta := loadCacheMeta(t, repoDir)
	meta.RefCommits = nil
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "vmake-cache.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := c.EnsureRef(context.Background(), gitURL, "refs/tags/v1.0.0", first.Commit); err != nil {
		t.Fatalf("EnsureRef local hit: %v", err)
	}
	if got := loadCacheMeta(t, repoDir).RefCommits["refs/tags/v1.0.0"]; got != first.Commit {
		t.Fatalf("RefCommits[refs/tags/v1.0.0] = %q, want %q", got, first.Commit)
	}
}

// TestObjectCacheRecordCommitSkipsFallback checks the best-effort record path
// never widens to a ref-tip fetch when the by-SHA fetch is refused.
func TestObjectCacheRecordCommitSkipsFallback(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	commitFile(t, src, "lib.c", "int val = 2;\n", "v2")
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	c := NewObjectCache(cache)
	repoDir := seedCacheNamespace(t, c, gitURL)

	bogus := "1111111111111111111111111111111111111111"
	if err := c.RecordCommit(context.Background(), gitURL, bogus); err == nil {
		t.Fatal("RecordCommit accepted an unknown commit")
	}
	if remote := gitOutput(t, repoDir, "for-each-ref", "--format=%(refname)", "refs/remotes/origin"); remote != "" {
		t.Fatalf("best-effort record fetched ref tips: %q", remote)
	}
}

// TestObjectCacheFallbackFetchIsDepthLimited checks the by-SHA fallback stays
// depth-limited: an existing namespace keeps only the ref tips, never the full
// history.
func TestObjectCacheFallbackFetchIsDepthLimited(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	commitFile(t, src, "lib.c", "int val = 2;\n", "v2")
	commitFile(t, src, "lib.c", "int val = 3;\n", "v3")

	cache := t.TempDir()
	c := NewObjectCache(cache)
	gitURL := fileURL(t, src)
	repoDir := seedCacheNamespace(t, c, gitURL)

	bogus := "1111111111111111111111111111111111111111"
	if err := c.EnsureCommit(context.Background(), gitURL, bogus); err == nil {
		t.Fatal("EnsureCommit accepted an unknown commit")
	}
	if got := gitOutput(t, repoDir, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("fallback fetch is not depth-limited: is-shallow=%s", got)
	}
	if got := gitOutput(t, repoDir, "rev-list", "--count", "--all"); got != "1" {
		t.Fatalf("fallback downloaded history: %s commits", got)
	}
	if remote := gitOutput(t, repoDir, "for-each-ref", "--format=%(refname)", "refs/remotes/origin"); !strings.Contains(remote, "refs/remotes/origin/") {
		t.Fatalf("bounded fallback did not fetch ref tips: %q", remote)
	}
}

// TestObjectCacheFailedEnsureCommitLeavesNoShell checks a failed first fetch
// does not leave an empty bare repository behind, while a namespace that
// already holds objects survives a later failure.
func TestObjectCacheFailedEnsureCommitLeavesNoShell(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	c := NewObjectCache(cache)
	bogus := "1111111111111111111111111111111111111111"
	if err := c.EnsureCommit(context.Background(), gitURL, bogus); err == nil {
		t.Fatal("EnsureCommit accepted an unknown commit")
	}
	if _, err := os.Stat(c.RepoDir(gitURL)); !os.IsNotExist(err) {
		t.Fatalf("failed EnsureCommit left a shell repository: %v", err)
	}

	if err := c.EnsureCommit(context.Background(), gitURL, gitOutput(t, src, "rev-parse", "HEAD")); err != nil {
		t.Fatalf("populated EnsureCommit: %v", err)
	}
	if err := c.EnsureCommit(context.Background(), gitURL, bogus); err == nil {
		t.Fatal("EnsureCommit accepted an unknown commit")
	}
	if _, err := os.Stat(c.RepoDir(gitURL)); err != nil {
		t.Fatalf("failed EnsureCommit removed a populated cache: %v", err)
	}
}

// TestEnsureSourceOfflineResolutionFromCache checks that an unreachable
// upstream can still resolve a ref the object cache recorded before.
func TestEnsureSourceOfflineResolutionFromCache(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	if _, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, src+"-away"); err != nil {
		t.Fatal(err)
	}

	res, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatalf("offline resolution from cache: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("content = %q", data)
	}
	if tag := gitOutput(t, res.SrcDir, "describe", "--tags", "--exact-match", "HEAD"); tag != "v1.0.0" {
		t.Fatalf("cached resolution lost the tag: %q", tag)
	}
}

// TestObjectCacheCommitOnlyFallbackMaterializes pins the cloneRefTree branch
// taken when the cache cannot refresh the ref but already anchors its commit:
// the tree is materialized from the commit alone. The cached tag is dropped
// from the repository and the upstream is removed, so success can only come
// from the anchor and the result must stay shallow and single-commit.
func TestObjectCacheCommitOnlyFallbackMaterializes(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	gitURL := fileURL(t, src)

	cache := t.TempDir()
	first, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	// Keep the recorded ref->commit mapping (CachedRef still resolves) but drop
	// the tag object, and make the upstream unreachable: EnsureRef must fail.
	repoDir := NewObjectCache(cache).RepoDir(gitURL)
	runGit(t, repoDir, "update-ref", "-d", "refs/tags/v1.0.0")
	if err := os.Rename(src, src+"-away"); err != nil {
		t.Fatal(err)
	}

	res, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{gitURL}, Version: "1.0.0", Ref: "v1.0.0", Commit: first.Commit})
	if err != nil {
		t.Fatalf("commit-only fallback: %v", err)
	}
	if res.Commit != first.Commit {
		t.Fatalf("commit = %s, want %s", res.Commit, first.Commit)
	}
	if data, _ := os.ReadFile(filepath.Join(res.SrcDir, "lib.c")); string(data) != "int val = 1;\n" {
		t.Fatalf("materialized content = %q", data)
	}
	// The ref could not be refreshed, so only the commit is present: the tag is
	// absent while the tree is shallow and holds exactly one commit.
	probe := exec.Command("git", "-C", res.SrcDir, "rev-parse", "v1.0.0^{commit}")
	if out, err := probe.CombinedOutput(); err == nil {
		t.Fatalf("commit-only materialization contains the tag: %s", out)
	}
	if got := gitOutput(t, res.SrcDir, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("commit-only tree is not shallow: %s", got)
	}
	if got := gitOutput(t, res.SrcDir, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("commit-only tree kept history: %s commits", got)
	}
}

func loadCacheMeta(t *testing.T, repoDir string) cacheMeta {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoDir, "vmake-cache.json"))
	if err != nil {
		t.Fatalf("read cache metadata: %v", err)
	}
	var meta cacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse cache metadata: %v", err)
	}
	return meta
}

// seedCacheNamespace initializes the bare cache repository for url without
// fetching, so a following failed EnsureCommit exercises the bounded fallback
// on an existing namespace instead of a freshly created one.
func seedCacheNamespace(t *testing.T, c *ObjectCache, url string) string {
	t.Helper()
	repoDir, meta, _, err := c.ensureRepo(context.Background(), normalizeCacheURL(url), url)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.saveMeta(repoDir, meta); err != nil {
		t.Fatal(err)
	}
	return repoDir
}

func assertCacheMetaRewritten(t *testing.T, repoDir, url string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoDir, "vmake-cache.json"))
	if err != nil {
		t.Fatalf("cache metadata not rewritten: %v", err)
	}
	var meta cacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("cache metadata not rewritten: %v", err)
	}
	if normalizeCacheURL(meta.URL) != normalizeCacheURL(url) {
		t.Fatalf("cache metadata url = %q, want %q", meta.URL, url)
	}
}
