package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestIsLocalGitURL(t *testing.T) {
	cases := map[string]bool{
		"/abs/path/repo":                  true,
		"./relative/repo":                 true,
		"relative/repo":                   true,
		"file:///abs/path/repo":           true,
		"https://git.busybox.net/busybox": false,
		"http://example.com/x.git":        false,
		"git@github.com:user/repo.git":    false,
		"ssh://git@host/x.git":            false,
		"git://host/x.git":                false,
	}
	for url, want := range cases {
		if got := isLocalGitURL(url); got != want {
			t.Errorf("isLocalGitURL(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestEnsureURLRefreshesExistingClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	src := t.TempDir()
	runGit(t, src, "init", "-q", "-b", "master")
	runGit(t, src, "config", "user.email", "test@example.com")
	runGit(t, src, "config", "user.name", "test")

	foo := filepath.Join(src, "foo.txt")
	if err := os.WriteFile(foo, []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "add", "-A")
	runGit(t, src, "commit", "-q", "-m", "v1")

	m := NewSourceManager(t.TempDir(), t.TempDir())
	srcDir, err := m.EnsureURL(src)
	if err != nil {
		t.Fatalf("first EnsureURL: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(srcDir, "foo.txt")); string(got) != "v1\n" {
		t.Fatalf("initial content = %q, want v1", got)
	}

	if err := os.WriteFile(foo, []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "add", "-A")
	runGit(t, src, "commit", "-q", "-m", "v2")

	srcDir2, err := m.EnsureURL(src)
	if err != nil {
		t.Fatalf("second EnsureURL: %v", err)
	}
	if srcDir2 != srcDir {
		t.Errorf("second EnsureURL path = %q, want %q", srcDir2, srcDir)
	}
	if got, _ := os.ReadFile(filepath.Join(srcDir2, "foo.txt")); string(got) != "v2\n" {
		t.Errorf("refreshed content = %q, want v2", got)
	}
}
