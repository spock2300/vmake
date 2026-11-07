package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestURLReadsOrigin(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	mgr := NewRepoManager(root)

	if url, err := mgr.URL("missing"); err != nil || url != "" {
		t.Fatalf("missing repository = %q, %v", url, err)
	}

	plain := filepath.Join(root, "plain")
	if err := os.MkdirAll(plain, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.URL("plain"); err == nil {
		t.Fatal("non-git repository URL read succeeded")
	}

	clone := filepath.Join(root, "official")
	if err := os.MkdirAll(clone, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, clone, "init", "-q")
	if url, err := mgr.URL("official"); err != nil || url != "" {
		t.Fatalf("repository without origin = %q, %v", url, err)
	}
	const origin = "https://example.invalid/official.git"
	runGit(t, clone, "remote", "add", "origin", origin)
	if url, err := mgr.URL("official"); err != nil || url != origin {
		t.Fatalf("repository origin = %q, %v, want %q", url, err, origin)
	}
}
