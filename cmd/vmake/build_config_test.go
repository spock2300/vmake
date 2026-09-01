package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureGitignoreIdempotent(t *testing.T) {
	dir := t.TempDir()
	gitignore := filepath.Join(dir, ".gitignore")

	ensureGitignore(dir)
	data1, _ := os.ReadFile(gitignore)

	ensureGitignore(dir)
	data2, _ := os.ReadFile(gitignore)

	if string(data1) != string(data2) {
		t.Errorf("ensureGitignore should not modify existing file")
	}
	if !stringsContains(string(data2), "vmake_deps") {
		t.Errorf(".gitignore should contain vmake_deps, got %q", string(data2))
	}
}

func TestEnsureGitignorePreservesExistingContent(t *testing.T) {
	dir := t.TempDir()
	gitignore := filepath.Join(dir, ".gitignore")
	original := "*.o\nbuild/\n"
	_ = os.WriteFile(gitignore, []byte(original), 0644)

	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	data, _ := os.ReadFile(gitignore)
	if !stringsContains(string(data), "*.o") {
		t.Errorf("existing content should be preserved, got %q", string(data))
	}
	if !stringsContains(string(data), "vmake_deps") {
		t.Errorf("vmake_deps should be appended, got %q", string(data))
	}
}

func TestEnsureGitignoreNegatedPatternNotCounted(t *testing.T) {
	dir := t.TempDir()
	gitignore := filepath.Join(dir, ".gitignore")
	_ = os.WriteFile(gitignore, []byte("!vmake_deps/\n"), 0644)

	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	data, _ := os.ReadFile(gitignore)
	content := string(data)
	if !stringsContains(content, "!vmake_deps/") {
		t.Errorf("negated pattern should be preserved, got %q", content)
	}
	if !stringsContains(content, "\nvmake_deps/") {
		t.Errorf("vmake_deps/ should still be appended after a negated pattern, got %q", content)
	}
}

func TestEnsureGitignoreExistingEntryNoAppend(t *testing.T) {
	dir := t.TempDir()
	gitignore := filepath.Join(dir, ".gitignore")
	original := "build/\nvmake_deps/\n"
	_ = os.WriteFile(gitignore, []byte(original), 0644)

	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	data, _ := os.ReadFile(gitignore)
	if string(data) != original {
		t.Errorf("existing vmake_deps entry should prevent append, got %q", string(data))
	}
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && sortStringSearch(s, sub)
}

func sortStringSearch(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
