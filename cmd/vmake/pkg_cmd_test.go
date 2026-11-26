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

func TestPkgCleanReleasesOutputsAndTree(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "vmake.json"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".vmake"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".vmake", "local.json"), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(project, ".vmake_deps", "official", "zlib")
	out := filepath.Join(tree, "out", "owner", "key", "build", "zlib.o")
	src := filepath.Join(tree, "src", "tasks.c")
	state := filepath.Join(tree, "state.json")
	for _, path := range []string{out, src, state} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("content\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"VMAKE_CACHE=" + canonicalTempDir(t)}
	if stdout, stderr, code := runCLI(t, project, canonicalTempDir(t), env, "pkg", "clean", "official/zlib"); code != 0 {
		t.Fatalf("pkg clean exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("output %s survived pkg clean", out)
	}
	for _, path := range []string{src, state} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("source %s was removed by pkg clean: %v", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if stdout, stderr, code := runCLI(t, project, canonicalTempDir(t), env, "pkg", "clean", "official/zlib", "--all"); code != 0 {
		t.Fatalf("pkg clean --all exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatalf("tree %s survived pkg clean --all", tree)
	}
}
