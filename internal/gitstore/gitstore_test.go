package gitstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateName(t *testing.T) {
	for _, name := range []string{"official", "myorg", "test-repo", "a.b"} {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", "../evil", "..\\evil", "a/b", "a\\b", "C:", "a:b", "a@b", " name", "name ", "name.", "CON", "nul.txt", "\x00"} {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) accepted an invalid name", name)
		}
	}
}

func TestRemoveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	store := New(filepath.Join(root, "extensions"))
	sentinel := filepath.Join(root, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"..", "../..", "../sentinel"} {
		if err := store.Remove(name); err == nil {
			t.Fatalf("Remove(%q) succeeded", name)
		}
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel was removed: %v", err)
	}
}

func TestAddRejectsTraversal(t *testing.T) {
	store := New(t.TempDir())
	called := false
	clone := func(string, string) error {
		called = true
		return nil
	}
	if err := store.Add("../evil", "url", clone); err == nil {
		t.Fatal("Add accepted a traversing name")
	}
	if called {
		t.Fatal("clone was called for an invalid name")
	}
}
