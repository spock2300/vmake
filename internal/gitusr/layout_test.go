package gitusr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func TestLayoutFromGitPath(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "usr", "bin"))
	mustMkdir(t, filepath.Join(root, "cmd"))
	mustMkdir(t, filepath.Join(root, "mingw64", "bin"))

	other := t.TempDir()
	mustMkdir(t, filepath.Join(other, "shims"))

	backslashed := strings.ReplaceAll(filepath.Join(root, "cmd", "git.exe"), "/", `\`)

	cases := []struct {
		name     string
		gitPath  string
		wantRoot string
		wantMsys string
		wantOK   bool
	}{
		{"cmd wrapper", filepath.Join(root, "cmd", "git.exe"), root, "mingw64", true},
		{"msystem bin", filepath.Join(root, "mingw64", "bin", "git.exe"), root, "mingw64", true},
		{"backslash separators", backslashed, root, "mingw64", true},
		{"unrelated shim", filepath.Join(other, "shims", "git.exe"), "", "", false},
		{"no usr/bin", filepath.Join(other, "cmd", "git.exe"), "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotRoot, gotMsys, gotOK := layoutFromGitPath(tc.gitPath)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			// Derivation works in slash space on every host; callers convert
			// back with filepath.FromSlash.
			if toSlash(gotRoot) != toSlash(tc.wantRoot) || gotMsys != tc.wantMsys {
				t.Fatalf("layout = (%q, %q), want (%q, %q)", gotRoot, gotMsys, tc.wantRoot, tc.wantMsys)
			}
		})
	}
}

func TestLayoutFromExecPath(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "usr", "bin"))
	mustMkdir(t, filepath.Join(root, "ucrt64", "libexec", "git-core"))

	other := t.TempDir()
	mustMkdir(t, filepath.Join(other, "unknown", "libexec", "git-core"))

	cases := []struct {
		name     string
		execPath string
		wantRoot string
		wantMsys string
		wantOK   bool
	}{
		{"ucrt64", filepath.Join(root, "ucrt64", "libexec", "git-core"), root, "ucrt64", true},
		{"unknown msystem", filepath.Join(other, "unknown", "libexec", "git-core"), "", "", false},
		{"missing dir", filepath.Join(root, "nope", "libexec", "git-core"), "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotRoot, gotMsys, gotOK := layoutFromExecPath(tc.execPath)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if toSlash(gotRoot) != toSlash(tc.wantRoot) || gotMsys != tc.wantMsys {
				t.Fatalf("layout = (%q, %q), want (%q, %q)", gotRoot, gotMsys, tc.wantRoot, tc.wantMsys)
			}
		})
	}
}

func TestUserlandDirs(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "usr", "bin"))
	mustMkdir(t, filepath.Join(root, "mingw64", "bin"))

	got := userlandDirs(root, "mingw64")
	want := []string{
		filepath.Join(root, "usr", "bin"),
		filepath.Join(root, "mingw64", "bin"),
	}
	if len(got) != len(want) {
		t.Fatalf("dirs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dirs = %v, want %v", got, want)
		}
	}

	if dirs := userlandDirs(root, "ucrt64"); len(dirs) != 1 {
		t.Fatalf("missing msystem bin should be skipped, got %v", dirs)
	}
}
