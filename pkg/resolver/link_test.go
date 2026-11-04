package resolver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/repo"
)

const linkPkgMain = `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {}
`

func writeLinkGitRepo(t *testing.T, files map[string]string) string {
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
	gitCmd(t, dir, "init", "-q", "-b", "master")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-q", "-m", "v1")
	return dir
}

func newLinkTestResolver(t *testing.T, repoName, template, linkPkg, linkPath string) (*Resolver, string) {
	t.Helper()
	reposDir := t.TempDir()
	depsDir := t.TempDir()
	cacheDir := t.TempDir()
	mgr := repo.NewRepoManager(reposDir)
	if err := mgr.AddNative(repoName, template); err != nil {
		t.Fatal(err)
	}
	r := newTrustedTestResolver(mgr, depsDir)
	r.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	r.SetSourceLinks(map[string]string{linkPkg: linkPath})
	return r, depsDir
}

func TestResolveLinkedNativePackageAndSubPackages(t *testing.T) {
	link := writeLinkGitRepo(t, map[string]string{
		"build.go":       linkPkgMain,
		"sub/build.go":   linkPkgMain,
		"sub/header.txt": "sub",
	})
	r, depsDir := newLinkTestResolver(t, "testnative", filepath.Join(filepath.Dir(link), "{name}"), "testnative/sdk", link)

	root := localRequiringSources(t, "root", "testnative/sdk >=99.0.0", "testnative/sdk/sub")
	if err := r.ResolveAll([]buildscript.Source{*root}); err != nil {
		t.Fatal(err)
	}
	node := r.Graph().Packages["testnative/sdk"]
	if node == nil || node.Source == nil || node.Source.Link == nil {
		t.Fatalf("linked native node missing: %+v", node)
	}
	if !node.IsNative() {
		t.Fatal("linked native package must remain native")
	}
	if node.Source.Link.Path != link || node.Source.Dir != link {
		t.Fatalf("link path = %q dir = %q, want %q", node.Source.Link.Path, node.Source.Dir, link)
	}
	if !node.Source.Link.Native || !node.Source.Link.LocalSource {
		t.Fatalf("link flags = %+v", node.Source.Link)
	}
	if node.Native.Selected != LocalLinkVersion {
		t.Fatalf("native selected = %q, want %q", node.Native.Selected, LocalLinkVersion)
	}
	sub := r.Graph().Packages["testnative/sdk/sub"]
	if sub == nil || sub.Source.Link == nil || sub.Source.Link.Path != filepath.Join(link, "sub") {
		t.Fatalf("linked sub-package = %+v", sub)
	}
	if entries, err := os.ReadDir(filepath.Join(depsDir, "testnative")); err == nil && len(entries) > 0 {
		t.Fatalf("linked package should not materialize under %s", filepath.Join(depsDir, "testnative"))
	}
}

func TestResolveLinkedNativeBypassesRemoteTrust(t *testing.T) {
	link := writeLinkGitRepo(t, map[string]string{"build.go": linkPkgMain})
	r, _ := newLinkTestResolver(t, "testnative", filepath.Join(filepath.Dir(link), "{name}"), "testnative/sdk", link)
	r.SetTrustChecker(func(buildscript.Source) error { return fmt.Errorf("untrusted") })
	root := localRequiringSources(t, "root", "testnative/sdk")
	if err := r.ResolveAll([]buildscript.Source{*root}); err != nil {
		t.Fatalf("linked native build.go should bypass remote trust: %v", err)
	}
}

func TestResolveLinkedNativeRejectsPatches(t *testing.T) {
	link := writeLinkGitRepo(t, map[string]string{
		"build.go": `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.AddPatches("patches/fix.patch")
}
`,
	})
	r, _ := newLinkTestResolver(t, "testnative", filepath.Join(filepath.Dir(link), "{name}"), "testnative/sdk", link)
	root := localRequiringSources(t, "root", "testnative/sdk")
	err := r.ResolveAll([]buildscript.Source{*root})
	if err == nil || !strings.Contains(err.Error(), "declares patches") {
		t.Fatalf("error = %v, want patch rejection", err)
	}
}

func TestResolveLinkedRegistryKeepsWrapper(t *testing.T) {
	upstream := writeLinkGitRepo(t, map[string]string{"src/foo.c": "int foo(void) { return 0; }\n"})
	reg := t.TempDir()
	buildGo := fmt.Sprintf(`package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit(%q)
		p.AddVersion("1.0.0", "v1")
	})
}
`, upstream)
	path := filepath.Join(reg, "packages", "z", "zlib", "build.go")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(buildGo), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, reg, "init", "-q", "-b", "master")
	gitCmd(t, reg, "config", "user.email", "test@example.com")
	gitCmd(t, reg, "config", "user.name", "test")
	gitCmd(t, reg, "add", "-A")
	gitCmd(t, reg, "commit", "-q", "-m", "v1")

	reposDir, depsDir, cacheDir := t.TempDir(), t.TempDir(), t.TempDir()
	mgr := repo.NewRepoManager(reposDir)
	if err := mgr.Add("testreg", reg); err != nil {
		t.Fatal(err)
	}
	r := newTrustedTestResolver(mgr, depsDir)
	r.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	r.SetSourceLinks(map[string]string{"testreg/zlib": upstream})

	root := localRequiringSources(t, "root", "testreg/zlib >=99.0.0")
	if err := r.ResolveAll([]buildscript.Source{*root}); err != nil {
		t.Fatal(err)
	}
	node := r.Graph().Packages["testreg/zlib"]
	if node == nil || node.Source == nil || node.Source.Link == nil {
		t.Fatalf("linked registry node missing: %+v", node)
	}
	if node.Source.Link.Native {
		t.Fatal("registry link must keep the registry wrapper")
	}
	if !node.Source.Link.LocalSource {
		t.Fatal("registry link must mark the local source")
	}
	if node.Source.Link.Path != upstream {
		t.Fatalf("link path = %q, want %q", node.Source.Link.Path, upstream)
	}
	if !strings.Contains(node.Source.Dir, "repos") && !strings.Contains(node.Source.Dir, "testreg") {
		t.Fatalf("registry wrapper source dir = %q", node.Source.Dir)
	}
	if node.Native != nil {
		t.Fatal("registry link must not become native")
	}
}
