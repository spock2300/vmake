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

func TestDevRootsUsedLocallyAndIgnoredExternally(t *testing.T) {
	sdkFiles := map[string]string{
		"vmake.json":           `{"dev_roots": ["//tests/runner"]}`,
		"lib/build.go":         emptyPackageMain,
		"tests/runner/build.go": requirePackageMain("//lib"),
	}
	localSDK := canonicalTempDir(t)
	writeProjectFiles(t, localSDK, sdkFiles)
	localResolver := newTrustedTestResolver(repo.NewRepoManager(canonicalTempDir(t)), canonicalTempDir(t))
	defer localResolver.Close()
	if err := localResolver.ResolveProject(localSDK, nil); err != nil {
		t.Fatalf("local ResolveProject with dev_roots failed: %v", err)
	}
	if localResolver.Graph().Packages["//tests/runner"] == nil || localResolver.Graph().Packages["//lib"] == nil {
		t.Fatalf("local dev_roots packages = %v", localResolver.Graph().Order)
	}

	bare := writeContainerRepo(t, sdkFiles)
	extResolver := newContainerTestResolver(t, bare)
	project := writeLocalProject(t, map[string]string{"testnative/mother": "*"}, nil, "testnative/mother/lib")
	if err := extResolver.ResolveProject(project, nil); err != nil {
		t.Fatalf("external ResolveProject failed: %v", err)
	}
	if extResolver.Graph().Packages["testnative/mother/lib"] == nil {
		t.Fatal("expected testnative/mother/lib in graph")
	}
	if extResolver.Graph().Packages["testnative/mother/tests/runner"] != nil {
		t.Fatal("dev_roots package must not be loaded when consumed externally")
	}
}

func TestContainerExportsWhitelistAndSingleIdentity(t *testing.T) {
	bare := writeContainerRepo(t, map[string]string{
		"vmake.json": `{
			"exports": {
				"core": "//system/core",
				"graphics": "//sdk/graphics"
			}
		}`,
		"sdk/graphics/build.go": requirePackageMain("//system/freertos/lvgl", "//system/core"),
		"system/core/build.go":  emptyPackageMain,
		"system/freertos/lvgl/build.go": emptyPackageMain,
	})

	r := newContainerTestResolver(t, bare)
	project := writeLocalProject(t, map[string]string{"testnative/mother": "*"}, nil, "testnative/mother/graphics")
	if err := r.ResolveProject(project, nil); err != nil {
		t.Fatalf("ResolveProject(exported): %v", err)
	}
	gfx := r.Graph().Packages["testnative/mother/graphics"]
	if gfx == nil || gfx.RelPath != "sdk/graphics" || !gfx.Exported {
		t.Fatalf("graphics node = %+v", gfx)
	}
	if api.BindPackageName(gfx.Pkg) != "testnative/mother/graphics" {
		t.Fatalf("graphics pkg name = %q, want testnative/mother/graphics", api.BindPackageName(gfx.Pkg))
	}
	core := r.Graph().Packages["testnative/mother/core"]
	if core == nil || core.RelPath != "system/core" || !core.Exported {
		t.Fatalf("core node = %+v", core)
	}
	if r.Graph().Packages["testnative/mother/system/core"] != nil {
		t.Fatal("exported package was loaded twice under its internal path")
	}
	lvgl := r.Graph().Packages["testnative/mother/system/freertos/lvgl"]
	if lvgl == nil || lvgl.Exported {
		t.Fatalf("unexported internal dep = %+v", lvgl)
	}
	if err := r.Graph().CheckExportVisibility("//", "testnative/mother/system/freertos/lvgl"); err == nil {
		t.Fatal("CheckExportVisibility should reject external access to non-exported package")
	}
	if err := r.Graph().CheckExportVisibility("testnative/mother/graphics", "testnative/mother/system/freertos/lvgl"); err != nil {
		t.Fatalf("intra-container CheckExportVisibility should succeed, got %v", err)
	}

	r.Close()
	r2 := newContainerTestResolver(t, bare)
	badProject := writeLocalProject(t, map[string]string{"testnative/mother": "*"}, nil, "testnative/mother/system/freertos/lvgl")
	if err := r2.ResolveProject(badProject, nil); err == nil || !strings.Contains(err.Error(), "not exported") {
		t.Fatalf("expected non-exported error, got %v", err)
	}
	r2.Close()

	emptyBare := writeContainerRepo(t, map[string]string{
		"vmake.json":   `{"exports": {}}`,
		"lib/build.go": emptyPackageMain,
	})
	rEmpty := newContainerTestResolver(t, emptyBare)
	emptyProj := writeLocalProject(t, map[string]string{"testnative/mother": "*"}, nil, "testnative/mother/lib")
	if err := rEmpty.ResolveProject(emptyProj, nil); err == nil || !strings.Contains(err.Error(), "not exported") {
		t.Fatalf("expected empty exports {} to deny external import, got %v", err)
	}
	rEmpty.Close()

	collBare := writeContainerRepo(t, map[string]string{
		"vmake.json":        `{"exports": {"core": "//sdk/core"}}`,
		"sdk/core/build.go": requirePackageMain("//core"),
		"core/build.go":     emptyPackageMain,
	})
	rColl := newContainerTestResolver(t, collBare)
	collProj := writeLocalProject(t, map[string]string{"testnative/mother": "*"}, nil, "testnative/mother/core")
	if err := rColl.ResolveProject(collProj, nil); err == nil || !strings.Contains(err.Error(), "collides with export") {
		t.Fatalf("expected export collision error, got %v", err)
	}
	rColl.Close()
}

func TestLocalPathAndSelfContainedManifestRepos(t *testing.T) {
	project := canonicalTempDir(t)
	writeProjectFiles(t, project, map[string]string{
		"vmake.json": `{
			"roots": ["//app"],
			"modules": {
				"vendor/sdk": {"path": "./third_party/sdk"}
			}
		}`,
		"app/build.go":                            requirePackageMain("vendor/sdk/core"),
		"third_party/sdk/vmake.json":              `{"exports": {"core": "//internal/core"}}`,
		"third_party/sdk/internal/core/build.go":  emptyPackageMain,
	})
	rPath := newTrustedTestResolver(repo.NewRepoManager(canonicalTempDir(t)), canonicalTempDir(t))
	defer rPath.Close()
	if err := rPath.ResolveProject(project, nil); err != nil {
		t.Fatalf("ResolveProject(local path container): %v", err)
	}
	c := rPath.Graph().Containers["vendor/sdk"]
	if c == nil || !c.IsLocalPath() || c.Dir != filepath.Join(project, "third_party", "sdk") {
		t.Fatalf("local path container = %+v", c)
	}
	core := rPath.Graph().Packages["vendor/sdk/core"]
	if core == nil || core.RelPath != "internal/core" || !core.Exported {
		t.Fatalf("vendor/sdk/core = %+v", core)
	}

	linkedSDK := canonicalTempDir(t)
	writeProjectFiles(t, linkedSDK, map[string]string{
		"vmake.json":               `{"exports": {"core": "//linked_core"}}`,
		"linked_core/build.go":     emptyPackageMain,
	})
	gitCmd(t, linkedSDK, "init", "-q", "-b", "master")
	gitCmd(t, linkedSDK, "config", "user.email", "test@example.com")
	gitCmd(t, linkedSDK, "config", "user.name", "test")
	gitCmd(t, linkedSDK, "add", "-A")
	gitCmd(t, linkedSDK, "commit", "-q", "-m", "init")
	rLink := newTrustedTestResolver(repo.NewRepoManager(canonicalTempDir(t)), canonicalTempDir(t))
	rLink.SetSourceLinks(map[string]string{"vendor/sdk": linkedSDK})
	defer rLink.Close()
	if err := rLink.ResolveProject(project, nil); err != nil {
		t.Fatalf("ResolveProject(linked override of local path container): %v", err)
	}
	if lc := rLink.Graph().Containers["vendor/sdk"]; lc == nil || !lc.IsLinked() || lc.Dir != linkedSDK {
		t.Fatalf("linked override container = %+v, want Dir=%s", lc, linkedSDK)
	}

	conflictProj := canonicalTempDir(t)
	writeProjectFiles(t, conflictProj, map[string]string{
		"vmake.json": `{
			"roots": ["//app"],
			"modules": {
				"vendor/sub": {"path": "./sub"},
				"vendor/sdk": {"path": "./sdk_a"}
			}
		}`,
		"app/build.go":        requirePackageMain("vendor/sdk/lib", "vendor/sub/user"),
		"sdk_a/lib/build.go":  emptyPackageMain,
		"sdk_b/lib/build.go":  emptyPackageMain,
		"sub/vmake.json":      `{"modules": {"vendor/sdk": {"path": "../sdk_b"}}}`,
		"sub/user/build.go":   requirePackageMain("vendor/sdk/lib"),
	})
	rConf := newTrustedTestResolver(repo.NewRepoManager(canonicalTempDir(t)), canonicalTempDir(t))
	defer rConf.Close()
	if err := rConf.ResolveProject(conflictProj, nil); err == nil {
		t.Fatal("expected conflict/escape error for conflicting local path containers")
	}

	bare := writeContainerRepo(t, map[string]string{
		"sub/build.go": emptyPackageMain,
	})
	template := strings.TrimSuffix(bare, "mother.git") + "{name}.git"
	selfProj := canonicalTempDir(t)
	writeProjectFiles(t, selfProj, map[string]string{
		"vmake.json": fmt.Sprintf(`{
			"roots": ["//app"],
			"repos": {"selforg": {"native": %q}},
			"modules": {"selforg/mother": ">=1.0.0"}
		}`, template),
		"app/build.go": requirePackageMain("selforg/mother/sub"),
	})
	var seenRepoURL string
	rSelf := NewResolver(repo.NewRepoManager(canonicalTempDir(t)), canonicalTempDir(t))
	rSelf.SetSourceManager(repo.NewSourceManager(canonicalTempDir(t), canonicalTempDir(t)))
	rSelf.SetTrustChecker(func(src buildscript.Source) error {
		seenRepoURL = src.RepoURL
		return nil
	})
	defer rSelf.Close()
	if err := rSelf.ResolveProject(selfProj, nil); err != nil {
		t.Fatalf("ResolveProject(self-contained repos): %v", err)
	}
	if seenRepoURL != template {
		t.Fatalf("trust checker saw RepoURL = %q, want %q", seenRepoURL, template)
	}
	if rSelf.Graph().Packages["selforg/mother/sub"] == nil {
		t.Fatal("expected selforg/mother/sub in graph")
	}

	overrideBare := writeContainerRepo(t, map[string]string{
		"override_only/build.go": emptyPackageMain,
	})
	overrideTemplate := strings.TrimSuffix(overrideBare, "mother.git") + "{name}.git"
	overrideReposDir := canonicalTempDir(t)
	overrideMgr := repo.NewRepoManager(overrideReposDir)
	if err := overrideMgr.AddNative("selforg", overrideTemplate); err != nil {
		t.Fatal(err)
	}
	overrideProj := canonicalTempDir(t)
	writeProjectFiles(t, overrideProj, map[string]string{
		"vmake.json": fmt.Sprintf(`{
			"roots": ["//app"],
			"repos": {"selforg": {"native": %q}},
			"modules": {"selforg/mother": ">=1.0.0"}
		}`, template),
		"app/build.go": requirePackageMain("selforg/mother/override_only"),
	})
	rOver := newTrustedTestResolver(overrideMgr, canonicalTempDir(t))
	rOver.SetSourceManager(repo.NewSourceManager(canonicalTempDir(t), canonicalTempDir(t)))
	defer rOver.Close()
	if err := rOver.ResolveProject(overrideProj, nil); err != nil {
		t.Fatalf("global repo override failed: %v", err)
	}
	if rOver.Graph().Packages["selforg/mother/override_only"] == nil {
		t.Fatal("expected global repo override to take precedence over manifest repos")
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

func TestLocalPathContainerAllowsSiblingRelativePath(t *testing.T) {
	root := canonicalTempDir(t)
	project := filepath.Join(root, "app")
	writeProjectFiles(t, project, map[string]string{
		"vmake.json": `{
			"roots": ["//"],
			"modules": {
				"vendor/mid": {"path": "../mid"}
			}
		}`,
		"build.go": requirePackageMain("vendor/mid/api"),
	})
	writeProjectFiles(t, filepath.Join(root, "mid"), map[string]string{
		"vmake.json": `{
			"exports": {"api": "//api"},
			"modules": {
				"vendor/shared": {"path": "../shared"}
			}
		}`,
		"api/build.go": requirePackageMain("vendor/shared/util"),
	})
	writeProjectFiles(t, filepath.Join(root, "shared"), map[string]string{
		"util/build.go": emptyPackageMain,
	})

	r := NewResolver(nil, filepath.Join(project, ".vmake_deps"))
	if err := r.ResolveProject(project, nil); err != nil {
		t.Fatalf("local-declared path container should allow sibling .. path: %v", err)
	}
	if r.Graph().Packages["vendor/shared/util"] == nil {
		t.Fatalf("expected vendor/shared/util to resolve, got %v", r.Graph().Packages)
	}
}

func TestRemoteContainerRejectsSymlinkEscapingPackageDir(t *testing.T) {
	outside := canonicalTempDir(t)
	writeProjectFiles(t, outside, map[string]string{
		"build.go": emptyPackageMain,
	})
	bare := writeContainerRepo(t, map[string]string{
		"vmake.json":  `{"exports": {"ok": "//ok"}}`,
		"ok/build.go": emptyPackageMain,
	})
	r := newContainerTestResolver(t, bare)
	project := writeLocalProject(t, map[string]string{"testnative/mother": "*"}, nil, "testnative/mother/ok")
	if err := r.ResolveProject(project, nil); err != nil {
		t.Fatalf("initial resolve failed: %v", err)
	}
	containerDir := r.Graph().Containers["testnative/mother"].Dir
	escapeLink := filepath.Join(containerDir, "escape")
	if err := os.Symlink(outside, escapeLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(containerDir, "vmake.json"), []byte(`{"exports": {"escape": "//escape"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	r2 := newTrustedTestResolver(r.repoMgr, r.depsDir)
	r2.SetSourceManager(r.sourceMgr)
	defer r2.Close()
	proj2 := writeLocalProject(t, map[string]string{"testnative/mother": "*"}, nil, "testnative/mother/escape")
	err := r2.ResolveProject(proj2, nil)
	if err == nil || !strings.Contains(err.Error(), "escapes container") {
		t.Fatalf("expected symlink escape rejection in remote container, got %v", err)
	}
}
