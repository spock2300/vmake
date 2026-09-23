package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsCommitSHAcceptsSHA256(t *testing.T) {
	if !isCommitSHA(strings.Repeat("a", 64)) {
		t.Fatal("a 64-hex commit id must be recognized")
	}
	if !isCommitSHA(strings.Repeat("0", 40)) {
		t.Fatal("a 40-hex commit id must be recognized")
	}
	if isCommitSHA(strings.Repeat("a", 41)) || isCommitSHA(strings.Repeat("z", 40)) {
		t.Fatal("invalid commit ids must be rejected")
	}
}

func TestEnsureURLRefPinsTagAndCommitOffline(t *testing.T) {
	upstream := writePatchableSource(t)
	runGit(t, upstream, "tag", "v1.0.0")
	firstCommit, err := GetCurrentCommit(upstream)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(upstream, "lib.c"), []byte("int val = 2;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, upstream, "commit", "-qam", "v2")
	secondCommit, err := GetCurrentCommit(upstream)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewSourceManager(t.TempDir(), t.TempDir())
	head, err := manager.EnsureURL(upstream)
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.EnsureURLRef(upstream, "v1.0.0", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if first == head {
		t.Fatal("tag source reused newer HEAD")
	}
	if commit, err := GetCurrentCommit(first); err != nil || commit != firstCommit {
		t.Fatalf("selected tag = %s, %v", commit, err)
	}
	cold := NewSourceManager(t.TempDir(), t.TempDir())
	byCommit, err := cold.EnsureURLRef(upstream, firstCommit, firstCommit, false)
	if err != nil {
		t.Fatal(err)
	}
	if commit, err := GetCurrentCommit(byCommit); err != nil || commit != firstCommit {
		t.Fatalf("selected commit = %s, %v", commit, err)
	}
	runGit(t, upstream, "tag", "-f", "v1.0.0")
	second, err := manager.EnsureURLRef(upstream, "v1.0.0", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if commit, err := GetCurrentCommit(second); err != nil || commit != secondCommit {
		t.Fatalf("explicit refresh = %s, %v", commit, err)
	}
	if err := os.Rename(upstream, filepath.Join(t.TempDir(), "offline")); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ expected, want string }{{firstCommit, first}, {"", second}} {
		got, err := manager.EnsureURLRef(upstream, "v1.0.0", test.expected, false)
		if err != nil || got != test.want {
			t.Fatalf("offline expected %s = %s, %v", test.expected, got, err)
		}
	}
	if _, err := manager.EnsureURLRef(upstream, "v1.0.0", strings.Repeat("0", 40), false); err == nil {
		t.Fatal("incorrect locked commit was accepted")
	}
}
