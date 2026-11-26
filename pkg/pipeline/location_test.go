package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

func locationTestContext(t *testing.T, nodes ...*resolver.PackageNode) *RuntimeContext {
	t.Helper()
	deps := t.TempDir()
	r := resolver.NewResolver(nil, deps)
	for _, node := range nodes {
		r.Graph().Packages[node.ID] = node
		r.Graph().Order = append(r.Graph().Order, node.ID)
	}
	return &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Paths: &Paths{DepsDir: deps}}
}

func locationTestNode(name, container, rel string, kind resolver.SourceKind, dir string) *resolver.PackageNode {
	origin := api.SourceRemote
	if kind == resolver.SourceKindLocal {
		origin = api.SourceLocal
	}
	src := buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, origin)
	src.Container = container
	src.RelPath = rel
	return resolver.NewPackageNode(name, container, rel, kind, src, api.NewPackage().SetName(name))
}

func TestSourceWorkspacePlacementMatrix(t *testing.T) {
	root := t.TempDir()

	t.Run("local plain", func(t *testing.T) {
		node := locationTestNode("//plain", "//", "plain", resolver.SourceKindLocal, filepath.Join(root, "plain"))
		ctx := locationTestContext(t, node)
		source := ctx.packageSource("//plain", node)
		if source.PackageRoot() != filepath.Join(root, "plain") || source.SourceTree() != source.PackageRoot() {
			t.Fatalf("source = %+v", source)
		}
		if source.TreeRoot() != "" {
			t.Fatalf("plain local tree root = %q, want empty", source.TreeRoot())
		}
		if source.Disposition() != SourceDispositionUnlinked {
			t.Fatalf("disposition = %s, want unlinked", source.Disposition())
		}
	})

	t.Run("local SetGit", func(t *testing.T) {
		node := locationTestNode("//git", "//", "git", resolver.SourceKindLocal, filepath.Join(root, "git"))
		node.Pkg.SetGit("https://example.invalid/git.git")
		ctx := locationTestContext(t, node)
		source := ctx.packageSource("//git", node)
		if source.PackageRoot() != filepath.Join(root, "git") {
			t.Fatalf("package root = %q", source.PackageRoot())
		}
		if source.SourceTree() != filepath.Join(root, "git", "src") {
			t.Fatalf("source tree = %q", source.SourceTree())
		}
		if source.TreeRoot() != filepath.Join(ctx.Paths.DepsDir, "local", build.MemberDir("git")) {
			t.Fatalf("tree root = %q", source.TreeRoot())
		}
		if source.BuildRoot() != filepath.Join(root, "git") {
			t.Fatalf("build root = %q", source.BuildRoot())
		}
	})

	t.Run("native member plain", func(t *testing.T) {
		ctx := locationTestContext(t)
		ownerRoot := filepath.Join(ctx.Paths.DepsDir, "repo", "sdk")
		node := locationTestNode("repo/sdk/sub", "repo/sdk", "sub", resolver.SourceKindNative, filepath.Join(ownerRoot, "src", "sub"))
		ctx.DepGraph.Packages[node.ID] = node
		ctx.DepGraph.Order = append(ctx.DepGraph.Order, node.ID)
		source := ctx.packageSource("repo/sdk/sub", node)
		if source.OwnerTreeRoot() != ownerRoot {
			t.Fatalf("owner tree = %q, want %q", source.OwnerTreeRoot(), ownerRoot)
		}
		if source.PackageRoot() != filepath.Join(ownerRoot, "src", "sub") || source.SourceTree() != source.PackageRoot() {
			t.Fatalf("source = %+v", source)
		}
		if source.TreeRoot() != ownerRoot {
			t.Fatalf("tree root = %q", source.TreeRoot())
		}
		if source.BuildRoot() != node.Source.Dir {
			t.Fatalf("build root = %q, want %q", source.BuildRoot(), node.Source.Dir)
		}
	})

	t.Run("native member with own SetGit always uses third_party", func(t *testing.T) {
		node := locationTestNode("repo/sdk/third/lvgl", "repo/sdk", "third/lvgl", resolver.SourceKindNative, "/tmp/remote/third/lvgl")
		node.Pkg.SetGit("https://example.invalid/lvgl.git")
		ctx := locationTestContext(t, node)
		source := ctx.packageSource("repo/sdk/third/lvgl", node)
		wantTP := filepath.Join(ctx.Paths.DepsDir, "repo", "sdk", "third_party", build.MemberDir("third/lvgl"))
		if source.ThirdPartyRoot() != wantTP || source.TreeRoot() != wantTP || source.SourceTree() != filepath.Join(wantTP, "src") {
			t.Fatalf("own-Git native member source = %+v, want third_party %q", source, wantTP)
		}
		if source.Linked() {
			t.Fatalf("unlinked own-Git member must have Linked=false")
		}
	})

	t.Run("directly linked native member", func(t *testing.T) {
		node := locationTestNode("link/sdk/sub", "link/sdk", "sub", resolver.SourceKindNative, "/tmp/link/sdk/sub")
		node.Pkg.SetGit("https://example.invalid/sub.git")
		node.Source.Link = &buildscript.SourceLink{Path: "/tmp/external/sub", Native: true, LocalSource: true}
		ctx := locationTestContext(t, node)
		source := ctx.packageSource("link/sdk/sub", node)
		if !source.Linked() || !source.ContainerLinked() || source.PackageRoot() != "/tmp/link/sdk/sub" || source.SourceTree() != "/tmp/external/sub" {
			t.Fatalf("source = %+v", source)
		}
		if source.TreeRoot() != filepath.Join(ctx.Paths.DepsDir, "link", "sdk") {
			t.Fatalf("tree root = %q", source.TreeRoot())
		}
		if source.Disposition() != SourceDispositionLinked {
			t.Fatalf("disposition = %s, want source-linked", source.Disposition())
		}
	})

	t.Run("plain member of linked container", func(t *testing.T) {
		node := locationTestNode("link/sdk/plain", "link/sdk", "plain", resolver.SourceKindNative, "/tmp/link/sdk/plain")
		node.Source.Link = &buildscript.SourceLink{Path: node.Source.Dir, Native: true, LocalSource: true}
		ctx := locationTestContext(t, node)
		source := ctx.packageSource("link/sdk/plain", node)
		if !source.Linked() || !source.ContainerLinked() {
			t.Fatalf("source = %+v, want container-linked", source)
		}
		if source.Disposition() != SourceDispositionContainerLinked {
			t.Fatalf("disposition = %s, want container-linked", source.Disposition())
		}
	})

	t.Run("linked native container", func(t *testing.T) {
		node := locationTestNode("link/sdk", "link/sdk", "", resolver.SourceKindNative, "/tmp/link/sdk")
		node.Source.Link = &buildscript.SourceLink{Path: node.Source.Dir, Native: true, LocalSource: true}
		ctx := locationTestContext(t, node)
		source := ctx.packageSource("link/sdk", node)
		if !source.Linked() || !source.ContainerLinked() {
			t.Fatalf("source = %+v, want container-linked", source)
		}
		if source.Disposition() != SourceDispositionContainerLinked {
			t.Fatalf("disposition = %s, want container-linked", source.Disposition())
		}
	})

	t.Run("linked container own-Git member", func(t *testing.T) {
		node := locationTestNode("link/sdk/kernel", "link/sdk", "system/freertos/kernel", resolver.SourceKindNative, "/tmp/link/system/freertos/kernel")
		node.Pkg.SetGit("https://example.invalid/kernel.git")
		node.Source.Link = &buildscript.SourceLink{Path: node.Source.Dir, Native: true, LocalSource: false}
		ctx := locationTestContext(t, node)
		source := ctx.packageSource("link/sdk/kernel", node)
		wantTP := filepath.Join(ctx.Paths.DepsDir, "link", "sdk", "third_party", build.MemberDir("system/freertos/kernel"))
		if source.Linked() || source.ThirdPartyRoot() != wantTP || source.SourceTree() != filepath.Join(wantTP, "src") {
			t.Fatalf("linked-container own-Git source = %+v, want third_party %q", source, wantTP)
		}
		if source.Disposition() != SourceDispositionContainerLinkedOwnGit {
			t.Fatalf("disposition = %s, want container-linked-own-git", source.Disposition())
		}
	})

	t.Run("registry package", func(t *testing.T) {
		registryDir := filepath.Join(root, "repos", "official", "packages", "t", "tinyexpr")
		node := locationTestNode("official/tinyexpr", "official", "tinyexpr", resolver.SourceKindRegistry, registryDir)
		ctx := locationTestContext(t, node)
		source := ctx.packageSource("official/tinyexpr", node)
		wantRoot := filepath.Join(ctx.Paths.DepsDir, "official", "tinyexpr")
		if source.OwnerTreeRoot() != wantRoot || source.PackageRoot() != filepath.Join(wantRoot, "src") {
			t.Fatalf("registry source = %+v", source)
		}
		if source.BuildRoot() != registryDir {
			t.Fatalf("registry build root = %q, want %q", source.BuildRoot(), registryDir)
		}
	})
}

func TestSourceWorkspaceReleaseSourceResolvesExportAlias(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "consumer")
	sdkDir := filepath.Join(root, "sdk")
	depsDir := filepath.Join(projectDir, ".vmake_deps")
	cacheDir := filepath.Join(root, "cache")

	if err := os.MkdirAll(sdkDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "vmake.json"), []byte(`{"exports":{"lvgl":"//system/freertos/lvgl"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	localCfg := &config.LocalConfig{Links: map[string]string{"org/sdk": sdkDir}}
	if err := config.SaveLocal(projectDir, localCfg); err != nil {
		t.Fatal(err)
	}

	sourceMgr := repo.NewSourceManager(depsDir, cacheDir)
	ownerRoot := sourceMgr.TreeDir(repo.PackageTreeKey("org", "sdk"))
	realTree := filepath.Join(ownerRoot, "third_party", build.MemberDir("system/freertos/lvgl"))
	wrongTree := filepath.Join(ownerRoot, "third_party", build.MemberDir("lvgl"))
	for _, dir := range []string{realTree, wrongTree} {
		if err := os.MkdirAll(filepath.Join(dir, "src"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "src", "marker.c"), []byte("marker\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	workspace, err := NewSourceWorkspaceFromProject(projectDir, depsDir)
	if err != nil {
		t.Fatal(err)
	}
	source, err := workspace.Package("org/sdk/lvgl")
	if err != nil {
		t.Fatal(err)
	}
	if err := source.ReleaseSource(sourceMgr); err != nil {
		t.Fatalf("ReleaseSource: %v", err)
	}
	if _, err := os.Stat(filepath.Join(realTree, "src")); !os.IsNotExist(err) {
		t.Fatalf("expected aliased exported SetGit src dir %s to be removed, stat err = %v", filepath.Join(realTree, "src"), err)
	}
	if _, err := os.Stat(filepath.Join(wrongTree, "src", "marker.c")); err != nil {
		t.Fatalf("wrong alias tree was touched: %v", err)
	}
}

func TestIssueForMapsExportVisibilityError(t *testing.T) {
	got := IssueFor(&resolver.ExportVisibilityError{
		From:      "//app",
		To:        "org/sdk/internal/priv",
		Container: "org/sdk",
		Exported:  []string{"core"},
	})
	if got.Kind != build.IssueKindInternalVisibility {
		t.Fatalf("IssueFor(ExportVisibilityError).Kind = %q, want %q", got.Kind, build.IssueKindInternalVisibility)
	}
}
