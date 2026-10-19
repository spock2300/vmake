package repo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestGitNetworkTimeoutOverride(t *testing.T) {
	t.Setenv("VMAKE_GIT_TIMEOUT", "77")
	if got := gitNetworkTimeout(); got != 77*time.Second {
		t.Fatalf("VMAKE_GIT_TIMEOUT = %s, want 77s", got)
	}
	t.Setenv("VMAKE_GIT_TIMEOUT", "not-a-number")
	if got := gitNetworkTimeout(); got != 30*time.Minute {
		t.Fatalf("invalid VMAKE_GIT_TIMEOUT = %s, want default 30m", got)
	}
	t.Setenv("VMAKE_GIT_TIMEOUT", "")
	if got := gitNetworkTimeout(); got != 30*time.Minute {
		t.Fatalf("default timeout = %s, want 30m", got)
	}
}
