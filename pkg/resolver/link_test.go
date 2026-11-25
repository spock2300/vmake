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
	dir := canonicalTempDir(t)
	writeProjectFiles(t, dir, files)
	gitCmd(t, dir, "init", "-q", "-b", "master")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-q", "-m", "v1")
	return dir
}

func newLinkTestResolver(t *testing.T, repoName, template string, links map[string]string) (*Resolver, string) {
	t.Helper()
	reposDir := canonicalTempDir(t)
	depsDir := canonicalTempDir(t)
	cacheDir := canonicalTempDir(t)
	mgr := repo.NewRepoManager(reposDir)
	if err := mgr.AddNative(repoName, template); err != nil {
		t.Fatal(err)
	}
	r := newTrustedTestResolver(mgr, depsDir)
	r.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	r.SetSourceLinks(links)
	t.Cleanup(r.Close)
	return r, depsDir
}

func TestResolveLinkedNativeContainerWithoutRootBuildGo(t *testing.T) {
	link := writeLinkGitRepo(t, map[string]string{
		"sub/build.go":   linkPkgMain,
		"sub/header.txt": "sub",
	})
	r, depsDir := newLinkTestResolver(t, "testnative", filepath.Join(filepath.Dir(link), "{name}"), map[string]string{
		"testnative/sdk": link,
	})

	project := writeLocalProject(t, map[string]string{"testnative/sdk": ">=99.0.0"}, nil, "testnative/sdk/sub")
	if err := r.ResolveProject(project, nil); err != nil {
		t.Fatal(err)
	}
	c := r.Graph().Containers["testnative/sdk"]
	if c == nil || !c.IsNative() || !c.IsLinked() {
		t.Fatalf("linked native container = %+v", c)
	}
	if c.Version != LocalLinkVersion {
		t.Fatalf("container version = %q, want %q", c.Version, LocalLinkVersion)
	}
	if r.Graph().Packages["testnative/sdk"] != nil {
		t.Fatal("container without root build.go must not enter Graph.Packages")
	}
	sub := r.Graph().Packages["testnative/sdk/sub"]
	if sub == nil || !sub.IsNative() || sub.Container != "testnative/sdk" || sub.RelPath != "sub" {
		t.Fatalf("linked member package = %+v", sub)
	}
	if sub.Source.Link == nil || !sub.Source.Link.Native || !sub.Source.Link.LocalSource || sub.Source.Link.Path != filepath.Join(link, "sub") {
		t.Fatalf("linked member source link = %+v", sub.Source.Link)
	}
	if entries, err := os.ReadDir(filepath.Join(depsDir, "testnative")); err == nil && len(entries) > 0 {
		t.Fatalf("linked container should not materialize under %s", filepath.Join(depsDir, "testnative"))
	}
}

func TestResolveLinkedNativeBypassesRemoteTrust(t *testing.T) {
	link := writeLinkGitRepo(t, map[string]string{"build.go": linkPkgMain})
	r, _ := newLinkTestResolver(t, "testnative", filepath.Join(filepath.Dir(link), "{name}"), map[string]string{
		"testnative/sdk": link,
	})
	r.SetTrustChecker(func(buildscript.Source) error { return fmt.Errorf("untrusted") })
	project := writeLocalProject(t, map[string]string{"testnative/sdk": "*"}, nil, "testnative/sdk")
	if err := r.ResolveProject(project, nil); err != nil {
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
	r, _ := newLinkTestResolver(t, "testnative", filepath.Join(filepath.Dir(link), "{name}"), map[string]string{
		"testnative/sdk": link,
	})
	project := writeLocalProject(t, map[string]string{"testnative/sdk": "*"}, nil, "testnative/sdk")
	err := r.ResolveProject(project, nil)
	if err == nil || !strings.Contains(err.Error(), "declares patches") {
		t.Fatalf("error = %v, want patch rejection", err)
	}
}

func TestResolveLinkedNativeAllowsPatchesOnOwnGitMemberAndSupportsMemberLink(t *testing.T) {
	upstream := writeLinkGitRepo(t, map[string]string{"src/lvgl.c": "int lvgl(void) { return 0; }\n"})
	sdk := writeLinkGitRepo(t, map[string]string{
		"third/lvgl/build.go": fmt.Sprintf(`package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit(%q)
		p.AddPatches("patches/fix.patch")
	})
}
`, upstream),
	})
	project := writeLocalProject(t, map[string]string{"testnative/sdk": "*"}, nil, "testnative/sdk/third/lvgl")

	// Container linked only: own-Git member keeps LocalSource=false, so patches are allowed.
	r1, _ := newLinkTestResolver(t, "testnative", filepath.Join(filepath.Dir(sdk), "{name}"), map[string]string{
		"testnative/sdk": sdk,
	})
	if err := r1.ResolveProject(project, nil); err != nil {
		t.Fatalf("own-Git member in linked container should allow patches: %v", err)
	}
	lvgl := r1.Graph().Packages["testnative/sdk/third/lvgl"]
	if lvgl == nil || !lvgl.HasOwnGit() || lvgl.Source.Link == nil || !lvgl.Source.Link.Native || lvgl.Source.Link.LocalSource {
		t.Fatalf("own-Git member link = %+v", lvgl)
	}
	r1.Close()

	// Both container AND own-Git member linked: LocalSource=true -> patches rejected.
	r2, _ := newLinkTestResolver(t, "testnative", filepath.Join(filepath.Dir(sdk), "{name}"), map[string]string{
		"testnative/sdk":            sdk,
		"testnative/sdk/third/lvgl": upstream,
	})
	err := r2.ResolveProject(project, nil)
	if err == nil || !strings.Contains(err.Error(), "declares patches") {
		t.Fatalf("directly linked own-Git member must reject patches, got %v", err)
	}
}

func TestResolveLinkedRegistryKeepsWrapper(t *testing.T) {
	upstream := writeLinkGitRepo(t, map[string]string{"src/foo.c": "int foo(void) { return 0; }\n"})
	reg := canonicalTempDir(t)
	buildGo := fmt.Sprintf(`package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit(%q)
		p.AddVersion("1.0.0", "v1")
	})
}
`, upstream)
	writeProjectFiles(t, reg, map[string]string{"packages/z/zlib/build.go": buildGo})
	gitCmd(t, reg, "init", "-q", "-b", "master")
	gitCmd(t, reg, "config", "user.email", "test@example.com")
	gitCmd(t, reg, "config", "user.name", "test")
	gitCmd(t, reg, "add", "-A")
	gitCmd(t, reg, "commit", "-q", "-m", "v1")

	reposDir, depsDir, cacheDir := canonicalTempDir(t), canonicalTempDir(t), canonicalTempDir(t)
	mgr := repo.NewRepoManager(reposDir)
	if err := mgr.Add("testreg", reg); err != nil {
		t.Fatal(err)
	}
	r := newTrustedTestResolver(mgr, depsDir)
	r.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	r.SetSourceLinks(map[string]string{"testreg/zlib": upstream})

	project := writeLocalProject(t,
		map[string]string{"testreg": "*"},
		map[string]string{"testreg/zlib": ">=99.0.0"},
		"testreg/zlib",
	)
	if err := r.ResolveProject(project, nil); err != nil {
		t.Fatal(err)
	}
	node := r.Graph().Packages["testreg/zlib"]
	if node == nil || node.Source == nil || node.Source.Link == nil {
		t.Fatalf("linked registry node missing: %+v", node)
	}
	if !node.IsRegistry() || node.Container != "testreg" {
		t.Fatalf("registry node metadata = %+v", node)
	}
	if node.Source.Link.Native || !node.Source.Link.LocalSource || node.Source.Link.Path != upstream {
		t.Fatalf("registry link = %+v", node.Source.Link)
	}
	if !strings.Contains(node.Source.Dir, "testreg") {
		t.Fatalf("registry wrapper source dir = %q", node.Source.Dir)
	}
}
