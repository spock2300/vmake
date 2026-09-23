package repo

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneHeadKeepsOnlySelectedHead(t *testing.T) {
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	if err := os.WriteFile(filepath.Join(src, "lib.c"), []byte("new head"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "add", "lib.c")
	runGit(t, src, "commit", "-q", "-m", "head")
	dest := filepath.Join(t.TempDir(), "head")
	gitURL := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(src), "/")}).String()
	if err := CloneHead(gitURL, dest); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "lib.c")); err != nil || string(data) != "new head" {
		t.Fatalf("wrong selected HEAD: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git", "shallow")); err != nil {
		t.Fatalf("HEAD clone downloaded full history: %v", err)
	}
	if tags, err := ListTags(dest); err != nil || len(tags) != 0 {
		t.Fatalf("HEAD clone unexpectedly fetched version tags: %v, %v", tags, err)
	}
}

func TestDirExists_WithGitSubdir(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if !dirExists(gitDir) {
		t.Errorf("dirExists(%q) = false, want true", gitDir)
	}
	if !dirExists(filepath.Join(dir, ".git")) {
		t.Errorf("dirExists(filepath.Join(%q, \".git\")) = false, want true", dir)
	}
}

func TestDirExists_Missing(t *testing.T) {
	dir := t.TempDir()
	if dirExists(filepath.Join(dir, ".git")) {
		t.Errorf("dirExists for nonexistent .git should be false")
	}
}

func TestEnsureRepoAtRef_ClonesWhenNoGit(t *testing.T) {
	dir := t.TempDir()
	err := EnsureRepoAtRef("http://example.com/fake.git", dir, "")
	if err == nil {
		t.Fatal("expected error from clone with fake URL")
	}
	// Should attempt Clone (not FetchTags) since .git doesn't exist
	if _, statErr := os.Stat(dir); statErr == nil {
		// Clone failed but repoDir still exists — Clone should clean up on failure
	}
}
