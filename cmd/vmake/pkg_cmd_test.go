package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureLocalLinkIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := ensureLocalLinkIgnored(dir); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalLinkIgnored(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), ".vmake/local.json"); got != 1 {
		t.Fatalf(".vmake/local.json entries = %d, content %q", got, data)
	}
}

func TestEnsureLocalLinkIgnoredHonorsVmakeDirectoryRule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte(".vmake/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalLinkIgnored(dir); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != ".vmake/\n" {
		t.Fatalf("gitignore changed for existing .vmake/ rule: %q", data)
	}
}

func TestListPackageSources(t *testing.T) {
	dir := t.TempDir()

	var empty bytes.Buffer
	listPackageSources(&empty, dir, nil)
	if got := empty.String(); got != "No package sources\n" {
		t.Fatalf("empty output = %q", got)
	}

	if err := os.MkdirAll(filepath.Join(dir, "official", "zlib"), 0755); err != nil {
		t.Fatal(err)
	}
	var noSource bytes.Buffer
	listPackageSources(&noSource, dir, nil)
	if got := noSource.String(); got != "No package sources\n" {
		t.Fatalf("output without src = %q", got)
	}
	var linkedNoSource bytes.Buffer
	listPackageSources(&linkedNoSource, dir, map[string]string{"official/zlib": "/tmp/zlib"})
	if got := linkedNoSource.String(); got != "No package sources\n" {
		t.Fatalf("output for linked package without src = %q", got)
	}

	if err := os.MkdirAll(filepath.Join(dir, "official", "zlib", "src"), 0755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	listPackageSources(&out, dir, map[string]string{"official/zlib": "/tmp/zlib"})
	got := out.String()
	for _, want := range []string{"Package sources:", "official/zlib (linked; stale materialized source)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q does not contain %q", got, want)
		}
	}
}
