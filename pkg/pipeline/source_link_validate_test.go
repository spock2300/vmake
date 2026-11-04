package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/repo"
)

func linkTestGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "init", "-q", "-b", "master")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "test")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "v1")
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestValidateSourceLinkNative(t *testing.T) {
	project, deps, repos := t.TempDir(), t.TempDir(), t.TempDir()
	mgr := repo.NewRepoManager(repos)
	if err := mgr.AddNative("testnative", filepath.Join(t.TempDir(), "{name}")); err != nil {
		t.Fatal(err)
	}
	link := linkTestGitRepo(t, map[string]string{
		"build.go": "package main\n\nfunc Main(p *interface{}) {}\n",
	})
	abs, err := ValidateSourceLink(context.Background(), project, deps, repos, "testnative/sdk", link)
	if err != nil {
		t.Fatal(err)
	}
	if abs != link {
		t.Fatalf("validated path = %q, want %q", abs, link)
	}
}

func TestValidateSourceLinkRegistry(t *testing.T) {
	project, deps, repos := t.TempDir(), t.TempDir(), t.TempDir()
	reg := t.TempDir()
	path := filepath.Join(reg, "packages", "z", "zlib", "build.go")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\n\nfunc Main(p *interface{}) {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, reg, "init", "-q", "-b", "master")
	gitRun(t, reg, "config", "user.email", "test@example.com")
	gitRun(t, reg, "config", "user.name", "test")
	gitRun(t, reg, "add", "-A")
	gitRun(t, reg, "commit", "-q", "-m", "v1")

	mgr := repo.NewRepoManager(repos)
	if err := mgr.Add("testreg", reg); err != nil {
		t.Fatal(err)
	}
	link := linkTestGitRepo(t, map[string]string{"foo.c": "int foo(void) { return 0; }\n"})
	abs, err := ValidateSourceLink(context.Background(), project, deps, repos, "testreg/zlib", link)
	if err != nil {
		t.Fatal(err)
	}
	if abs != link {
		t.Fatalf("validated path = %q, want %q", abs, link)
	}
}

func TestValidateSourceLinkRejectsInsideProject(t *testing.T) {
	project, deps, repos := t.TempDir(), t.TempDir(), t.TempDir()
	mgr := repo.NewRepoManager(repos)
	if err := mgr.AddNative("testnative", filepath.Join(t.TempDir(), "{name}")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "sdk")
	if err := os.MkdirAll(link, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(link, "build.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateSourceLink(context.Background(), project, deps, repos, "testnative/sdk", link); err == nil ||
		!strings.Contains(err.Error(), "outside") {
		t.Fatalf("error = %v, want outside rejection", err)
	}
}

func TestValidateSourceLinkRejectsSubPackage(t *testing.T) {
	if _, err := ValidateSourceLink(context.Background(), t.TempDir(), t.TempDir(), t.TempDir(), "testnative/sdk/sub", "/tmp"); err == nil {
		t.Fatal("sub-package link should fail")
	}
}
