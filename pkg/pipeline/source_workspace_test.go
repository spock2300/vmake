package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestSourceWorkspaceDiskPlacementResolvesLinkedContainerExportAlias(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "consumer")
	sdkDir := filepath.Join(root, "sdk")
	depsDir := filepath.Join(projectDir, ".vmake_deps")
	if err := os.MkdirAll(sdkDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "vmake.json"), []byte(`{"exports":{"kernel":"//system/freertos/kernel"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveLocal(projectDir, &config.LocalConfig{Links: map[string]string{"hl/hl-mcu-sdk": sdkDir}}); err != nil {
		t.Fatal(err)
	}

	workspace, err := NewSourceWorkspaceFromProject(projectDir, depsDir)
	if err != nil {
		t.Fatal(err)
	}
	source, err := workspace.Package("hl/hl-mcu-sdk/kernel")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(depsDir, "hl", "hl-mcu-sdk", "third_party", build.MemberDir("system/freertos/kernel"))
	if source.ThirdPartyRoot() != want {
		t.Fatalf("third-party root = %q, want %q", source.ThirdPartyRoot(), want)
	}
	if source.SourceTree() != filepath.Join(want, "src") {
		t.Fatalf("source tree = %q, want %q", source.SourceTree(), filepath.Join(want, "src"))
	}
	if source.Disposition() != SourceDispositionContainerLinkedOwnGit {
		t.Fatalf("disposition = %s, want container-linked-own-git", source.Disposition())
	}
}

func TestCheckoutManifestResolvesLinkedContainerExportAlias(t *testing.T) {
	upstream := initGitRepo(t, map[string]string{"tasks.c": "int tasks(void) { return 0; }\n"})
	commit, err := repo.GetCurrentCommit(upstream)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	projectDir := filepath.Join(root, "consumer")
	sdkDir := filepath.Join(root, "sdk")
	depsDir := filepath.Join(projectDir, ".vmake_deps")
	if err := os.MkdirAll(sdkDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "vmake.json"), []byte(`{"exports":{"kernel":"//system/freertos/kernel"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveLocal(projectDir, &config.LocalConfig{Links: map[string]string{"hl/hl-mcu-sdk": sdkDir}}); err != nil {
		t.Fatal(err)
	}

	mgr := repo.NewSourceManager(depsDir, filepath.Join(root, "cache"))
	manifest := &InstallManifest{Packages: []InstallManifestEntry{{
		Name:   "hl/hl-mcu-sdk/kernel",
		Source: "git",
		URL:    upstream,
		Ref:    commit,
	}}}
	if err := CheckoutManifest(projectDir, depsDir, mgr, func(repoName, pkgName string) *api.Package {
		return api.NewPackage().SetRepo(repoName).SetName(pkgName)
	}, manifest, "hl/hl-mcu-sdk/kernel"); err != nil {
		t.Fatal(err)
	}

	correct := filepath.Join(depsDir, "hl", "hl-mcu-sdk", "third_party", build.MemberDir("system/freertos/kernel"), "src", "tasks.c")
	if _, err := os.Stat(correct); err != nil {
		t.Fatalf("correct source %s was not materialized: %v", correct, err)
	}
	wrong := filepath.Join(depsDir, "hl", "hl-mcu-sdk", "third_party", build.MemberDir("kernel"))
	if _, err := os.Stat(wrong); !os.IsNotExist(err) {
		t.Fatalf("alias-derived third-party tree %s exists, stat err = %v", wrong, err)
	}
}

func TestPackageSourceReleaseSourceKeepsOutputsAndThirdParty(t *testing.T) {
	projectDir := t.TempDir()
	depsDir := filepath.Join(projectDir, ".vmake_deps")
	owner := filepath.Join(depsDir, "native", "sample")
	for _, path := range []string{
		filepath.Join(owner, "src", "source.c"),
		filepath.Join(owner, "state.json"),
		filepath.Join(owner, "out", "member", "key", "build", "artifact.a"),
		filepath.Join(owner, "third_party", "member", "src", "upstream.c"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("content\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newSourceWorkspaceAt(projectDir, depsDir, nil)
	source, err := workspace.Package("native/sample")
	if err != nil {
		t.Fatal(err)
	}
	mgr := repo.NewSourceManager(depsDir, t.TempDir())
	if err := source.ReleaseSource(mgr); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(owner, "src"), filepath.Join(owner, "state.json")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists", path)
		}
	}
	for _, path := range []string{
		filepath.Join(owner, "out", "member", "key", "build", "artifact.a"),
		filepath.Join(owner, "third_party", "member", "src", "upstream.c"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s was removed: %v", path, err)
		}
	}
}

func TestPackageSourceReleaseOutputsAndAll(t *testing.T) {
	projectDir := t.TempDir()
	depsDir := filepath.Join(projectDir, ".vmake_deps")
	owner := filepath.Join(depsDir, "native", "sample")
	out := filepath.Join(owner, "out", "member", "key", "build", "artifact.a")
	for _, path := range []string{out, filepath.Join(owner, "src", "source.c"), filepath.Join(owner, "third_party", "member", "src", "upstream.c")} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("content\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newSourceWorkspaceAt(projectDir, depsDir, nil)
	source, err := workspace.Package("native/sample")
	if err != nil {
		t.Fatal(err)
	}
	if err := source.ReleaseOutputs(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("output %s survived ReleaseOutputs", out)
	}
	if _, err := os.Stat(filepath.Join(owner, "src", "source.c")); err != nil {
		t.Fatalf("source was removed by ReleaseOutputs: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := source.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(owner); !os.IsNotExist(err) {
		t.Fatalf("owner tree %s survived ReleaseAll", owner)
	}
}

func TestPackageSourceReleaseLocalSetGitRemovesLinkAndTree(t *testing.T) {
	projectDir := t.TempDir()
	depsDir := filepath.Join(projectDir, ".vmake_deps")
	pkgDir := filepath.Join(projectDir, "app")
	localTree := filepath.Join(depsDir, "local", build.MemberDir("app"))
	if err := os.MkdirAll(filepath.Join(localTree, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localTree, "src", "app.c"), []byte("int app;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localTree, "state.json"), []byte(`{"commit":"abc"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(pkgDir, "src")
	if err := os.Symlink(filepath.Join(localTree, "src"), link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	workspace := newSourceWorkspaceAt(projectDir, depsDir, nil)
	source, err := workspace.Package("//app")
	if err != nil {
		t.Fatal(err)
	}
	mgr := repo.NewSourceManager(depsDir, t.TempDir())
	if err := source.ReleaseSource(mgr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("package src link %s survived ReleaseSource", link)
	}
	for _, path := range []string{filepath.Join(localTree, "src"), filepath.Join(localTree, "state.json")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived ReleaseSource", path)
		}
	}
}

func TestPackageSourceReleaseAllRemovesThirdPartyAndOutputs(t *testing.T) {
	projectDir := t.TempDir()
	depsDir := filepath.Join(projectDir, ".vmake_deps")
	owner := filepath.Join(depsDir, "org", "sdk")
	memberRoot := filepath.Join(owner, "third_party", build.MemberDir("system/freertos/kernel"))
	out := filepath.Join(owner, "out", build.MemberDir("system/freertos/kernel"), "key", "build", "artifact.a")
	for _, path := range []string{
		filepath.Join(owner, "src", "sdk.c"),
		filepath.Join(memberRoot, "src", "kernel.c"),
		filepath.Join(memberRoot, "state.json"),
		out,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("content\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newSourceWorkspaceAt(projectDir, depsDir, nil)
	source, err := workspace.Package("org/sdk/system/freertos/kernel")
	if err != nil {
		t.Fatal(err)
	}
	if err := source.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{memberRoot, out} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived ReleaseAll", path)
		}
	}
	if _, err := os.Stat(filepath.Join(owner, "src", "sdk.c")); err != nil {
		t.Fatalf("owner source was removed by member ReleaseAll: %v", err)
	}
}

func TestSourceWorkspaceRejectsSymlinkedTreeRoot(t *testing.T) {
	projectDir := t.TempDir()
	depsDir := filepath.Join(projectDir, ".vmake_deps")
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(depsDir, "native"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(depsDir, "native", "pkg")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	workspace := newSourceWorkspaceAt(projectDir, depsDir, nil)
	if _, err := workspace.Package("native/pkg"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Package error = %v, want symlink rejection", err)
	}
}

func TestPackageSourceBindKeepsLocalExplicitSrcDir(t *testing.T) {
	root := t.TempDir()
	node := locationTestNode("//plain", api.LocalContainerID, "plain", resolver.SourceKindLocal, filepath.Join(root, "plain"))
	ctx := locationTestContext(t, node)
	source := ctx.packageSource("//plain", node)
	node.Pkg.SetSrcDir("custom")
	if err := source.Bind(node.Pkg, api.PkgDirs{SourceDir: filepath.Join(root, "plain")}, "", false); err != nil {
		t.Fatal(err)
	}
	if got := node.Pkg.SrcDir(); got != "custom" {
		t.Fatalf("local plain SrcDir = %q, want the explicit relative value unchanged", got)
	}
}

func TestCleanAllLinkedOutputs(t *testing.T) {
	dep := t.TempDir()
	out := filepath.Join(dep, "native", "pkg", "out", "owner", "buildkey", "build")
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "artifact.o"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	newSourceWorkspaceAt("", dep, map[string]string{"native/pkg": "/tmp/linked"}).ReleaseOutputsAll()
	if _, err := os.Stat(filepath.Join(out, "artifact.o")); !os.IsNotExist(err) {
		t.Fatalf("linked output was not cleaned: %v", err)
	}
}

func TestReleaseOutputsAllFindsNestedTrees(t *testing.T) {
	dep := t.TempDir()
	writeTree := func(root string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(root, "out", "owner", "buildkey", "build")
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "artifact.o"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	top := filepath.Join(dep, "native", "root")
	nested := filepath.Join(top, "src", "member")
	writeTree(top)
	writeTree(nested)

	newSourceWorkspaceAt("", dep, nil).ReleaseOutputsAll()

	for _, root := range []string{top, nested} {
		out := filepath.Join(root, "out")
		owners, err := os.ReadDir(out)
		if err != nil {
			t.Fatalf("out under %s missing: %v", root, err)
		}
		for _, owner := range owners {
			builds, err := os.ReadDir(filepath.Join(out, owner.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if len(builds) != 0 {
				t.Fatalf("outputs under %s survived: %v", root, builds)
			}
		}
		if _, err := os.Stat(filepath.Join(root, "src")); err != nil {
			t.Fatalf("source under %s was removed: %v", root, err)
		}
	}
}

func TestSourceWorkspaceDiskDispositionForLinkedOwners(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	depsDir := filepath.Join(projectDir, ".vmake_deps")
	sdkDir := filepath.Join(root, "sdk")
	registryDir := filepath.Join(root, "registry-src")
	if err := os.MkdirAll(sdkDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "vmake.json"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(registryDir, 0755); err != nil {
		t.Fatal(err)
	}
	workspace := newSourceWorkspaceAt(projectDir, depsDir, map[string]string{
		"hl/hl-mcu-sdk": sdkDir,
		"official/zlib": registryDir,
	})
	native, err := workspace.Package("hl/hl-mcu-sdk")
	if err != nil {
		t.Fatal(err)
	}
	if native.Disposition() != SourceDispositionContainerLinked {
		t.Fatalf("native container disposition = %s, want container-linked", native.Disposition())
	}
	registry, err := workspace.Package("official/zlib")
	if err != nil {
		t.Fatal(err)
	}
	if registry.Disposition() != SourceDispositionLinked {
		t.Fatalf("registry link disposition = %s, want source-linked", registry.Disposition())
	}
}
