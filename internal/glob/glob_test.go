package glob

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/fs"
)

func TestRecursiveMatch(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []string{"src/top.c", "src/nested/inner.c", "src/nested/deep/last.c", "src/nested/skip.h", "other/unused.c"} {
		full := filepath.Join(dir, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, pattern := range []string{"src/**/*.c", "./src/**/*.c", "s*/**/*.c", "src/**/**/*.c", filepath.FromSlash("src/**/*.c")} {
		got, err := Match(pattern, dir)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{filepath.FromSlash("src/nested/deep/last.c"), filepath.FromSlash("src/nested/inner.c"), filepath.FromSlash("src/top.c")}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Match(%q) = %v, want %v", pattern, got, want)
		}
	}
	if _, err := Match("src/**/[", dir); err == nil {
		t.Fatal("malformed pattern was accepted")
	}
}

func TestRecursiveMatchFollowsSymlinks(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	root := t.TempDir()
	realRoot := filepath.Join(root, "real")
	if err := os.MkdirAll(filepath.Join(realRoot, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realRoot, "nested", "file.c"), []byte("c"), 0644); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(root, "src")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(linkRoot) })
	alias := filepath.Join(realRoot, "alias")
	if err := os.Symlink(filepath.Join(realRoot, "nested"), alias); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(alias) })
	for _, dir := range []string{realRoot, linkRoot} {
		got, err := Match("**/*.c", dir)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{filepath.Join("alias", "file.c"), filepath.Join("nested", "file.c")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Match from %s = %v, want %v", dir, got, want)
		}
	}
	cycle := filepath.Join(realRoot, "nested", "cycle")
	if err := os.Symlink(realRoot, cycle); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(cycle) })
	if _, err := Match("**/*.c", linkRoot); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestMatchPathNativeSeparators(t *testing.T) {
	for _, test := range []struct {
		pattern string
		name    string
		want    bool
	}{
		{"src/**/*.c", "src/top.c", true},
		{"src/**/*.c", "src/a/b/file.c", true},
		{"src/**/*.c", "src/a/file.h", false},
		{"s*/**/include/**/*.h", "src/lib/include/public/a.h", true},
		{"src/*.c", "src/nested/a.c", false},
		{"src/*/*.c", "src/nested/a.c", true},
		{"src/**", "src/a/b", true},
	} {
		if got := MatchPath(test.pattern, filepath.FromSlash(test.name)); got != test.want {
			t.Errorf("MatchPath(%q, %q) = %v", test.pattern, test.name, got)
		}
	}
}

func TestRecursiveMatchBrokenSymlink(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte("c"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "unused.h")
	if err := os.Symlink(filepath.Join(dir, "missing.h"), link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	got, err := Match("**/*.c", dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"main.c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("matches = %v, want %v", got, want)
	}
	if _, err := Match("**/*.h", dir); !os.IsNotExist(err) {
		t.Fatalf("matching broken symlink error = %v", err)
	}
}

func TestRecursiveMatchMissingRoot(t *testing.T) {
	got, err := Match("missing/**/*.c", t.TempDir())
	if err != nil || len(got) != 0 {
		t.Fatalf("missing root matches = %v, %v", got, err)
	}
}
