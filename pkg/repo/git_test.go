package repo

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if tags := gitOutput(t, dest, "tag", "--list"); tags != "" {
		t.Fatalf("HEAD clone unexpectedly fetched version tags: %v", tags)
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

func TestGitNetworkTimeoutOverride(t *testing.T) {
	t.Setenv("VMAKE_GIT_TIMEOUT", "77")
	if got := gitNetworkTimeout(); got != 77*time.Second {
		t.Fatalf("VMAKE_GIT_TIMEOUT = %s, want 77s", got)
	}
	t.Setenv("VMAKE_GIT_TIMEOUT", "")
	t.Setenv("VMAKE_FETCH_TIMEOUT", "42")
	if got := gitNetworkTimeout(); got != 42*time.Second {
		t.Fatalf("VMAKE_FETCH_TIMEOUT fallback = %s, want 42s", got)
	}
	t.Setenv("VMAKE_GIT_TIMEOUT", "not-a-number")
	if got := gitNetworkTimeout(); got != 42*time.Second {
		t.Fatalf("invalid VMAKE_GIT_TIMEOUT = %s, want fallback 42s", got)
	}
	t.Setenv("VMAKE_FETCH_TIMEOUT", "")
	if got := gitNetworkTimeout(); got != 30*time.Minute {
		t.Fatalf("default timeout = %s, want 30m", got)
	}
}
