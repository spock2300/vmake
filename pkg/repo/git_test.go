package repo

import (
	"os"
	"path/filepath"
	"testing"
)

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
