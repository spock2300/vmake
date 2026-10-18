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

const subPackageMain = `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {}
`

func requireSubPackageMain(deps ...string) string {
	quoted := make([]string, len(deps))
	for i, dep := range deps {
		quoted[i] = fmt.Sprintf("%q", dep)
	}
	return fmt.Sprintf(`package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires(%s)
	})
}
`, strings.Join(quoted, ", "))
}

func writeSubPackageRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	work := filepath.Join(t.TempDir(), "mother")
	for name, content := range files {
		path := filepath.Join(work, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	gitCmd(t, work, "init", "-q", "-b", "master")
	gitCmd(t, work, "config", "user.email", "test@example.com")
	gitCmd(t, work, "config", "user.name", "test")
	gitCmd(t, work, "add", "-A")
	gitCmd(t, work, "commit", "-q", "-m", "v1")
	gitCmd(t, work, "tag", "v1.0.0")
	bare := filepath.Join(t.TempDir(), "mother.git")
	gitCmd(t, work, "clone", "-q", "--bare", ".", bare)
	return bare
}

func writeBareBuildRepo(t *testing.T, dest string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "build.go"), []byte(subPackageMain), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, work, "init", "-q", "-b", "master")
	gitCmd(t, work, "config", "user.email", "test@example.com")
	gitCmd(t, work, "config", "user.name", "test")
	gitCmd(t, work, "add", "-A")
	gitCmd(t, work, "commit", "-q", "-m", "v1")
	gitCmd(t, work, "tag", "v1.0.0")
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, work, "clone", "-q", "--bare", ".", dest)
}

func newSubPackageTestResolver(t *testing.T, bare string) *Resolver {
	t.Helper()
	reposDir, depsDir, cacheDir := t.TempDir(), t.TempDir(), t.TempDir()
	mgr := repo.NewRepoManager(reposDir)
	template := strings.TrimSuffix(bare, "mother.git") + "{name}.git"
	if err := mgr.AddNative("testnative", template); err != nil {
		t.Fatal(err)
	}
	r := newTrustedTestResolver(mgr, depsDir)
	r.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	return r
}

func localRequiringSources(t *testing.T, name string, reqs ...string) *buildscript.Source {
	t.Helper()
	dir := t.TempDir()
	quoted := make([]string, len(reqs))
	for i, req := range reqs {
		quoted[i] = fmt.Sprintf("%q", req)
	}
	script := fmt.Sprintf(`package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires(%s)
	})
}
`, strings.Join(quoted, ", "))
	path := filepath.Join(dir, "build.go")
	if err := os.WriteFile(path, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	return buildscript.NewSource(name, path, dir, api.SourceLocal)
}

func TestResolveNativeSubPackagesLazily(t *testing.T) {
	bare := writeSubPackageRepo(t, map[string]string{
		"build.go":         subPackageMain,
		"sub_a/build.go":   requireSubPackageMain("sub_b"),
		"sub_b/build.go":   subPackageMain,
		"unused/build.go":  "package main\nfunc Main( {",
		"unused/nested.go": "package main\n",
	})
	r := newSubPackageTestResolver(t, bare)
	if err := r.ResolveAll([]buildscript.Source{*localRequiringSources(t, "root", "testnative/mother", "testnative/mother/sub_a")}); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"testnative/mother", "testnative/mother/sub_a", "testnative/mother/sub_b"} {
		if r.Graph().Packages[name] == nil {
			t.Fatalf("missing package %s in graph", name)
		}
	}
	if r.Graph().Packages["testnative/mother/unused"] != nil {
		t.Fatal("unused sub-package must not be loaded lazily")
	}
	owner := r.Graph().Packages["testnative/mother"]
	subA := r.Graph().Packages["testnative/mother/sub_a"]
	if !strings.HasSuffix(owner.Source.Dir, filepath.Join("testnative", "mother", "src")) {
		t.Fatalf("owner source dir = %s", owner.Source.Dir)
	}
	if !strings.HasSuffix(subA.Source.Dir, filepath.Join("testnative", "mother", "src", "sub_a")) {
		t.Fatalf("sub_a source dir = %s", subA.Source.Dir)
	}
	if !strings.HasPrefix(subA.Source.Dir, owner.Source.Dir+string(filepath.Separator)) {
		t.Fatalf("sub_a source does not live under owner: %s vs %s", subA.Source.Dir, owner.Source.Dir)
	}
	if subA.Pkg.Name != "testnative/mother/sub_a" {
		t.Fatalf("sub-package name = %q, want qualified name", subA.Pkg.Name)
	}
	if len(subA.Deps) != 1 || subA.Deps[0] != "testnative/mother/sub_b" {
		t.Fatalf("sub_a deps = %v, want [testnative/mother/sub_b]", subA.Deps)
	}
	if got := r.SubParents()["testnative/mother/unused"]; got != "testnative/mother" {
		t.Fatalf("discovered unused parent = %q", got)
	}
	if subA.Source.Origin != api.SourceRemote || subA.Source.Repo != "testnative" {
		t.Fatalf("sub-package source = %+v", subA.Source)
	}
}

func TestResolveSubPackageNestedChildShortName(t *testing.T) {
	bare := writeSubPackageRepo(t, map[string]string{
		"build.go":         subPackageMain,
		"a/build.go":       requireSubPackageMain("child"),
		"a/child/build.go": subPackageMain,
	})
	r := newSubPackageTestResolver(t, bare)
	if err := r.ResolveAll([]buildscript.Source{*localRequiringSources(t, "root", "testnative/mother", "testnative/mother/a")}); err != nil {
		t.Fatal(err)
	}
	node := r.Graph().Packages["testnative/mother/a"]
	if node == nil || len(node.Deps) != 1 || node.Deps[0] != "testnative/mother/a/child" {
		t.Fatalf("nested child deps = %v", node)
	}
}

func TestResolveSubPackageConstraintIgnored(t *testing.T) {
	bare := writeSubPackageRepo(t, map[string]string{
		"build.go":     subPackageMain,
		"sub/build.go": subPackageMain,
	})
	r := newSubPackageTestResolver(t, bare)
	if err := r.ResolveAll([]buildscript.Source{*localRequiringSources(t, "root",
		"testnative/mother", "testnative/mother/sub >=99.0")}); err != nil {
		t.Fatalf("sub-package constraint should be ignored: %v", err)
	}
	node := r.Graph().Packages["testnative/mother/sub"]
	if node == nil || node.Native != nil {
		t.Fatalf("sub-package node = %+v", node)
	}
}

func TestResolveSubPackageBeforeParentFailsWithHint(t *testing.T) {
	bare := writeSubPackageRepo(t, map[string]string{
		"build.go":     subPackageMain,
		"sub/build.go": subPackageMain,
	})
	r := newSubPackageTestResolver(t, bare)
	err := r.ResolveAll([]buildscript.Source{*localRequiringSources(t, "root", "testnative/mother/sub")})
	if err == nil {
		t.Fatal("referencing a sub-package before its parent should fail")
	}
	for _, want := range []string{"parent must be resolved first", "list the parent before the sub-package"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestResolveMissingSubPackageUnderResolvedParent(t *testing.T) {
	bare := writeSubPackageRepo(t, map[string]string{
		"build.go":     subPackageMain,
		"sub/build.go": subPackageMain,
	})
	r := newSubPackageTestResolver(t, bare)
	err := r.ResolveAll([]buildscript.Source{*localRequiringSources(t, "root",
		"testnative/mother", "testnative/mother/ghost")})
	if err == nil {
		t.Fatal("missing sub-package should fail")
	}
	if !strings.Contains(err.Error(), "not found under resolved parent package") {
		t.Errorf("error should identify the resolved parent, got: %v", err)
	}
	if strings.Contains(err.Error(), "parent must be resolved first") {
		t.Errorf("resolved-parent error should not ask to list the parent first: %v", err)
	}
}

func TestResolveSeparateNestedNativePackage(t *testing.T) {
	parentBare := writeSubPackageRepo(t, map[string]string{
		"build.go": subPackageMain,
	})
	nestedBare := filepath.Join(filepath.Dir(parentBare), "mother", "sub.git")
	writeBareBuildRepo(t, nestedBare)
	r := newSubPackageTestResolver(t, parentBare)
	if err := r.ResolveAll([]buildscript.Source{*localRequiringSources(t, "root",
		"testnative/mother", "testnative/mother/sub")}); err != nil {
		t.Fatalf("separate nested native package should resolve: %v", err)
	}
	if r.Graph().Packages["testnative/mother/sub"] == nil {
		t.Fatal("separate nested native package missing from graph")
	}
	if _, isSub := r.SubParents()["testnative/mother/sub"]; isSub {
		t.Fatal("separate nested native package must not be treated as a sub-package")
	}
}

func TestResolveRegistryPackageHasNoSubPackages(t *testing.T) {
	reposDir, depsDir, cacheDir := t.TempDir(), t.TempDir(), t.TempDir()
	pkgDir := filepath.Join(reposDir, "testreg", "packages", "m", "module")
	for name, content := range map[string]string{
		"build.go":     subPackageMain,
		"sub/build.go": subPackageMain,
	} {
		path := filepath.Join(pkgDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	r := newTrustedTestResolver(repo.NewRepoManager(reposDir), depsDir)
	r.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	if err := r.ResolveAll([]buildscript.Source{*localRequiringSources(t, "root", "testreg/module")}); err != nil {
		t.Fatal(err)
	}
	if r.Graph().Packages["testreg/module"] == nil {
		t.Fatal("registry package was not resolved")
	}
	if len(r.SubParents()) != 0 {
		t.Fatalf("registry package discovered sub-packages: %v", r.SubParents())
	}
	if r.Graph().Packages["testreg/module/sub"] != nil {
		t.Fatal("registry nested build.go must not become a sub-package")
	}
}

func TestScanSubPackagesFollowsSymlinkedCheckout(t *testing.T) {
	realDir := t.TempDir()
	for name, content := range map[string]string{
		"build.go":        subPackageMain,
		"nested/build.go": subPackageMain,
	} {
		path := filepath.Join(realDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	r := NewResolver(nil, t.TempDir())
	r.scanSubPackages("testnative/mother", link)
	src := r.sources["testnative/mother/nested"]
	if src == nil {
		t.Fatal("sub-package under a symlinked checkout was not discovered")
	}
	if src.Repo != "testnative" {
		t.Fatalf("source repo = %q, want testnative", src.Repo)
	}
	if got := r.SubParents()["testnative/mother/nested"]; got != "testnative/mother" {
		t.Fatalf("sub-parent = %q", got)
	}
	if r.Graph().Packages["testnative/mother/nested"] != nil {
		t.Fatal("scanSubPackages must not load the sub-package graph node")
	}
}
