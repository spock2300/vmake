package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
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
	if srcDir2 == srcDir {
		t.Errorf("different commits share source path %q", srcDir2)
	}
	if got, _ := os.ReadFile(filepath.Join(srcDir, "foo.txt")); string(got) != "v1\n" {
		t.Errorf("previous source changed to %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(srcDir2, "foo.txt")); string(got) != "v2\n" {
		t.Errorf("refreshed content = %q, want v2", got)
	}
}

func TestLinkProjectCreatesDirectoryLinks(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	m := NewSourceManager(t.TempDir(), t.TempDir())
	pkg := api.NewPackage().SetName("sample")
	pkg.Repo = "native"
	versionDir := m.versionDir(pkg, "1.0.0")
	if err := os.MkdirAll(filepath.Join(versionDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := m.linkProject(pkg, versionDir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"src", "out"} {
		link := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, name)
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink: %v", name, err)
		}
		if info, err := os.Stat(link); err != nil || !info.IsDir() {
			t.Fatalf("%s is not a usable directory link: %v", name, err)
		}
	}
	out := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "out", "result")
	if err := os.WriteFile(out, []byte("artifact"), 0644); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(versionDir, "out", "result")); err != nil || string(data) != "artifact" {
		t.Fatalf("global out = %q, %v", data, err)
	}
}

func TestEnsureVersionColdAndOffline(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	src := writePatchableSource(t)
	runGit(t, src, "config", "core.symlinks", "true")
	if err := os.Symlink("lib.c", filepath.Join(src, "alias.c")); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "add", "alias.c")
	runGit(t, src, "commit", "-q", "-m", "source symlink")
	runGit(t, src, "tag", "v1.0.0")
	pkg := api.NewPackage().SetRepo("native").SetName("sample").SetGit(src)
	pkg.SetVersions(map[string]string{"1.0.0": "v1.0.0"})
	m := NewSourceManager(t.TempDir(), t.TempDir())
	refs, err := m.EnsureRefsClone(pkg, false)
	if err != nil {
		t.Fatal(err)
	}
	tags, err := ListTags(refs)
	if err != nil {
		t.Fatal(err)
	}
	version, tag, err := SelectNativeVersion(FilterValidVersions(tags), ">=1.0.0")
	if err != nil || version != "1.0.0" || tag != "v1.0.0" {
		t.Fatalf("native selection = %s, %s, %v", version, tag, err)
	}
	commit, err := GetCurrentCommit(src)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.EnsureVersion(pkg, version, commit)
	if err != nil {
		t.Fatal(err)
	}
	if !m.HasMaterializedVersion(pkg, version) || first.Commit != commit {
		t.Fatalf("materialized result = %#v", first)
	}
	if data, err := os.ReadFile(filepath.Join(first.LocalSrc, "lib.c")); err != nil || string(data) != "int val = 1;\n" {
		t.Fatalf("cold source = %q, %v", data, err)
	}
	if info, err := os.Lstat(filepath.Join(first.LocalSrc, "alias.c")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("cached source symlink was not preserved: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(first.LocalSrc, "alias.c")); err != nil || string(data) != "int val = 1;\n" {
		t.Fatalf("cached source link = %q, %v", data, err)
	}
	if err := os.Rename(src, filepath.Join(t.TempDir(), "offline")); err != nil {
		t.Fatal(err)
	}
	if cachedRefs, err := m.EnsureRefsClone(pkg, false); err != nil || cachedRefs != refs {
		t.Fatalf("offline refs = %s, %v", cachedRefs, err)
	}
	other := NewSourceManager(t.TempDir(), m.globalDir)
	for _, manager := range []*SourceManager{m, other} {
		t.Cleanup(func() {
			_ = os.Remove(manager.localSrcPath(pkg))
			_ = os.Remove(filepath.Join(manager.sourcesDir, pkg.Repo, pkg.Name, "out"))
		})
		cached, err := manager.EnsureVersion(pkg, version, commit)
		if err != nil {
			t.Fatal(err)
		}
		if cached.VersionDir != first.VersionDir || cached.Commit != first.Commit {
			t.Fatalf("offline shared result = %#v, want %#v", cached, first)
		}
		out := filepath.Join(manager.sourcesDir, pkg.Repo, pkg.Name, "out")
		if info, err := os.Stat(out); err != nil || !info.IsDir() {
			t.Fatalf("offline out is not a directory: %v", err)
		}
	}
	if _, err := m.EnsureVersion(pkg, version, strings.Repeat("0", 40)); err == nil || !strings.Contains(err.Error(), "but expected") {
		t.Fatalf("expected-commit mismatch error = %v", err)
	}
}
