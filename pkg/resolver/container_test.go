package resolver

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/repo"
)

const emptyPackageMain = `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {}
`

func requirePackageMain(deps ...string) string {
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

func writeContainerRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	work := filepath.Join(canonicalTempDir(t), "mother")
	writeProjectFiles(t, work, files)
	gitCmd(t, work, "init", "-q", "-b", "master")
	gitCmd(t, work, "config", "user.email", "test@example.com")
	gitCmd(t, work, "config", "user.name", "test")
	gitCmd(t, work, "add", "-A")
	gitCmd(t, work, "commit", "-q", "-m", "v1")
	gitCmd(t, work, "tag", "v1.0.0")
	bare := filepath.Join(canonicalTempDir(t), "mother.git")
	gitCmd(t, work, "clone", "-q", "--bare", ".", bare)
	return bare
}

func newContainerTestResolver(t *testing.T, bare string) *Resolver {
	t.Helper()
	reposDir, depsDir, cacheDir := canonicalTempDir(t), canonicalTempDir(t), canonicalTempDir(t)
	mgr := repo.NewRepoManager(reposDir)
	template := strings.TrimSuffix(bare, "mother.git") + "{name}.git"
	if err := mgr.AddNative("testnative", template); err != nil {
		t.Fatal(err)
	}
	r := newTrustedTestResolver(mgr, depsDir)
	r.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	t.Cleanup(r.Close)
	return r
}

func TestResolveNativeContainerWithoutRootBuildGoLazily(t *testing.T) {
	bare := writeContainerRepo(t, map[string]string{
		"sub_a/build.go":   requirePackageMain("//sub_b"),
		"sub_b/build.go":   emptyPackageMain,
		"unused/build.go":  "package main\nfunc Main( {",
		"unused/nested.go": "package main\n",
	})
	r := newContainerTestResolver(t, bare)
	project := writeLocalProject(t, map[string]string{"testnative/mother": ">=1.0.0"}, nil, "testnative/mother/sub_a")
	if err := r.ResolveProject(project, nil); err != nil {
		t.Fatal(err)
	}

	c := r.Graph().Containers["testnative/mother"]
	if c == nil || !c.IsNative() || c.Version != "1.0.0" {
		t.Fatalf("container = %+v", c)
	}
	if r.Graph().Packages["testnative/mother"] != nil {
		t.Fatal("container without root build.go must not be added to Graph.Packages")
	}
	for _, name := range []string{"testnative/mother/sub_a", "testnative/mother/sub_b"} {
		node := r.Graph().Packages[name]
		if node == nil || !node.IsNative() || node.Container != "testnative/mother" {
			t.Fatalf("package %s = %+v", name, node)
		}
	}
	if r.Graph().Packages["testnative/mother/unused"] != nil {
		t.Fatal("unused package must not be loaded lazily")
	}
	subA := r.Graph().Packages["testnative/mother/sub_a"]
	if !strings.HasSuffix(subA.Source.Dir, filepath.Join("testnative", "mother", "src", "sub_a")) {
		t.Fatalf("sub_a source dir = %s", subA.Source.Dir)
	}
	if api.BindPackageName(subA.Pkg) != "testnative/mother/sub_a" {
		t.Fatalf("package name = %q, want canonical ID", api.BindPackageName(subA.Pkg))
	}
	if len(subA.Deps) != 1 || subA.Deps[0] != "testnative/mother/sub_b" {
		t.Fatalf("sub_a deps = %v, want [testnative/mother/sub_b]", subA.Deps)
	}
	if subA.Source.Origin != api.SourceRemote || subA.Source.Repo != "testnative" {
		t.Fatalf("sub_a source = %+v", subA.Source)
	}
}

func TestResolveNativeContainerWithOptionalRootBuildGo(t *testing.T) {
	bare := writeContainerRepo(t, map[string]string{
		"build.go":     requirePackageMain("//sub"),
		"sub/build.go": emptyPackageMain,
	})
	r := newContainerTestResolver(t, bare)
	project := writeLocalProject(t, map[string]string{"testnative/mother": "*"}, nil, "testnative/mother")
	if err := r.ResolveProject(project, nil); err != nil {
		t.Fatal(err)
	}
	root := r.Graph().Packages["testnative/mother"]
	if root == nil || !root.IsNative() || root.RelPath != "" {
		t.Fatalf("root package = %+v", root)
	}
	if len(root.Deps) != 1 || root.Deps[0] != "testnative/mother/sub" {
		t.Fatalf("root deps = %v, want [testnative/mother/sub]", root.Deps)
	}
}

func TestResolveRejectsUndeclaredExternalModule(t *testing.T) {
	bare := writeContainerRepo(t, map[string]string{
		"sub/build.go": emptyPackageMain,
	})
	r := newContainerTestResolver(t, bare)
	project := writeLocalProject(t, nil, nil, "testnative/mother/sub")
	err := r.ResolveProject(project, nil)
	if err == nil || !strings.Contains(err.Error(), `add the container to "modules" in vmake.json`) {
		t.Fatalf("expected undeclared module error, got %v", err)
	}
}

func TestResolveRejectsPhantomTransitiveImport(t *testing.T) {
	bare := writeContainerRepo(t, map[string]string{
		"build.go": emptyPackageMain,
	})
	baseDir := filepath.Dir(bare)
	writeTaggedNativeModule(t, filepath.Join(baseDir, "mid.git"), map[string]string{
		"vmake.json": `{"modules": {"testnative/mother": ">=1.0.0"}}`,
		"build.go":   requirePackageMain("testnative/mother"),
	}, "v1.0.0")

	r := newContainerTestResolver(t, bare)
	// Root declares mid, NOT mother, yet tries to require mother directly.
	project := writeLocalProject(t, map[string]string{"testnative/mid": ">=1.0.0"}, nil, "testnative/mid", "testnative/mother")
	err := r.ResolveProject(project, nil)
	if err == nil || !strings.Contains(err.Error(), `testnative/mother`) {
		t.Fatalf("phantom transitive import must fail, got %v", err)
	}
}

func TestResolveSeparateNestedNativeContainer(t *testing.T) {
	parentBare := writeContainerRepo(t, map[string]string{
		"build.go": emptyPackageMain,
	})
	nestedBare := filepath.Join(filepath.Dir(parentBare), "mother", "sub.git")
	writeTaggedNativeModule(t, nestedBare, map[string]string{"build.go": emptyPackageMain}, "v1.0.0")

	r := newContainerTestResolver(t, parentBare)
	project := writeLocalProject(t, map[string]string{
		"testnative/mother":     ">=1.0.0",
		"testnative/mother/sub": ">=1.0.0",
	}, nil, "testnative/mother", "testnative/mother/sub")
	if err := r.ResolveProject(project, nil); err != nil {
		t.Fatalf("separate nested native container should resolve: %v", err)
	}
	sub := r.Graph().Packages["testnative/mother/sub"]
	if sub == nil || sub.Container != "testnative/mother/sub" || sub.RelPath != "" {
		t.Fatalf("nested container node = %+v", sub)
	}
}

func TestResolveRegistryContainerAndSourcesConstraint(t *testing.T) {
	reposDir, depsDir, cacheDir := canonicalTempDir(t), canonicalTempDir(t), canonicalTempDir(t)
	pkgDir := filepath.Join(reposDir, "testreg", "packages", "m", "module")
	writeProjectFiles(t, pkgDir, map[string]string{
		"build.go": `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://example.invalid/module.git")
		p.AddVersion("1.0.0", "v1.0.0")
	})
}
`,
	})
	r := newTrustedTestResolver(repo.NewRepoManager(reposDir), depsDir)
	r.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))

	project := writeLocalProject(t,
		map[string]string{"testreg": "*"},
		map[string]string{"testreg/module": ">=1.0.0"},
		"testreg/module",
	)
	if err := r.ResolveProject(project, nil); err != nil {
		t.Fatal(err)
	}
	node := r.Graph().Packages["testreg/module"]
	if node == nil || !node.IsRegistry() || node.Container != "testreg" || !node.HasOwnGit() {
		t.Fatalf("registry package = %+v", node)
	}
	if len(node.Constraints) != 1 || node.Constraints[0] != ">=1.0.0" {
		t.Fatalf("source constraints = %v, want [>=1.0.0]", node.Constraints)
	}

	// Conflicting source constraint must fail.
	badProject := writeLocalProject(t,
		map[string]string{"testreg": "*"},
		map[string]string{"testreg/module": ">=2.0.0"},
		"testreg/module",
	)
	rBad := newTrustedTestResolver(repo.NewRepoManager(reposDir), depsDir)
	rBad.SetSourceManager(repo.NewSourceManager(depsDir, cacheDir))
	if err := rBad.ResolveProject(badProject, nil); err == nil {
		t.Fatal("unsatisfiable source constraint should fail")
	}
}

func TestResolveRegistryRejectsPackageInModulesSection(t *testing.T) {
	reposDir, depsDir := canonicalTempDir(t), canonicalTempDir(t)
	writeProjectFiles(t, filepath.Join(reposDir, "testreg", "packages", "m", "module"), map[string]string{
		"build.go": emptyPackageMain,
	})
	r := newTrustedTestResolver(repo.NewRepoManager(reposDir), depsDir)
	project := writeLocalProject(t, map[string]string{"testreg/module": "*"}, nil, "testreg/module")
	err := r.ResolveProject(project, nil)
	if err == nil || !strings.Contains(err.Error(), `is a registry repository`) {
		t.Fatalf("expected registry-in-modules guidance, got %v", err)
	}
}

func TestResolveProjectLazyLocalRoots(t *testing.T) {
	root := canonicalTempDir(t)
	writeProjectFiles(t, root, map[string]string{
		"vmake.json":      `{"roots": ["//app"]}`,
		"app/build.go":    requirePackageMain("//lib"),
		"lib/build.go":    emptyPackageMain,
		"unused/build.go": "package main\nfunc Main( {",
	})
	r := NewResolver(nil, filepath.Join(root, ".vmake_deps"))
	if err := r.ResolveProject(root, nil); err != nil {
		t.Fatalf("ResolveProject should lazily ignore unused local packages: %v", err)
	}
	if r.Graph().Packages["//app"] == nil || r.Graph().Packages["//lib"] == nil {
		t.Fatalf("expected //app and //lib, got %v", r.Graph().Packages)
	}
	if r.Graph().Packages["//unused"] != nil {
		t.Fatal("//unused must not be loaded")
	}
}
