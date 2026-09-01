package resolver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/repo"
)

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeNativeRepoWithVersions(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pkgDir := filepath.Join(root, "conflictpkg.git")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, pkgDir, "init", "-q", "-b", "master")
	gitCmd(t, pkgDir, "config", "user.email", "test@example.com")
	gitCmd(t, pkgDir, "config", "user.name", "test")

	buildGo := `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "build.go"), []byte(buildGo), 0644); err != nil {
		t.Fatal(err)
	}
	verFile := filepath.Join(pkgDir, "version.txt")
	if err := os.WriteFile(verFile, []byte("1.2.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, pkgDir, "add", "-A")
	gitCmd(t, pkgDir, "commit", "-q", "-m", "v1.2.0")
	gitCmd(t, pkgDir, "tag", "v1.2.0")

	if err := os.WriteFile(verFile, []byte("1.9.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, pkgDir, "add", "-A")
	gitCmd(t, pkgDir, "commit", "-q", "-m", "v1.9.0")
	gitCmd(t, pkgDir, "tag", "v1.9.0")

	return pkgDir
}

func localRequiringSource(t *testing.T, name, require string) *buildscript.Source {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf(`package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires(%q)
	})
}
`, require)
	if err := os.WriteFile(filepath.Join(dir, "build.go"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	return buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, api.SourceLocal)
}

func newNativeTestResolver(t *testing.T, pkgGitURL string) *Resolver {
	t.Helper()
	reposDir := t.TempDir()
	depsDir := t.TempDir()
	cacheDir := t.TempDir()

	repoMgr := repo.NewRepoManager(reposDir)
	template := strings.TrimSuffix(pkgGitURL, "conflictpkg.git") + "{name}.git"
	if err := repoMgr.AddNative("testnative", template); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(repoMgr, depsDir)
	r.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	return r
}

func TestResolveNativeConstraintConflictFails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	url := writeNativeRepoWithVersions(t)
	r := newNativeTestResolver(t, url)

	a := localRequiringSource(t, "a", "testnative/conflictpkg >=1.2")
	b := localRequiringSource(t, "b", "testnative/conflictpkg <1.5")

	err := r.ResolveAll([]buildscript.Source{*a, *b})
	if err == nil {
		t.Fatal("conflicting native constraints should fail resolution")
	}
	for _, want := range []string{"conflicting version constraints", "1.9.0", "<1.5", "b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestResolveNativeConstraintAgreementSucceeds(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	url := writeNativeRepoWithVersions(t)
	r := newNativeTestResolver(t, url)

	a := localRequiringSource(t, "a", "testnative/conflictpkg >=1.2")
	b := localRequiringSource(t, "b", "testnative/conflictpkg >=1.4")

	if err := r.ResolveAll([]buildscript.Source{*a, *b}); err != nil {
		t.Fatalf("compatible constraints should resolve: %v", err)
	}

	node := r.Graph().Packages["testnative/conflictpkg"]
	if node == nil || node.Native == nil {
		t.Fatal("native node missing after resolution")
	}
	if node.Native.Selected != "1.9.0" {
		t.Errorf("Selected = %q, want 1.9.0", node.Native.Selected)
	}
}
