package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
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

func TestPackageLocationMatrix(t *testing.T) {
	root := t.TempDir()

	t.Run("local plain", func(t *testing.T) {
		node := locationTestNode("//plain", "//", "plain", resolver.SourceKindLocal, filepath.Join(root, "plain"))
		ctx := locationTestContext(t, node)
		loc := ctx.packageLocation("//plain", node)
		if loc.PackageRoot != filepath.Join(root, "plain") || loc.SourceTree != loc.PackageRoot {
			t.Fatalf("loc = %+v", loc)
		}
		if loc.TreeRoot != "" {
			t.Fatalf("plain local tree root = %q, want empty", loc.TreeRoot)
		}
	})

	t.Run("local SetGit", func(t *testing.T) {
		node := locationTestNode("//git", "//", "git", resolver.SourceKindLocal, filepath.Join(root, "git"))
		node.Pkg.SetGit("https://example.invalid/git.git")
		ctx := locationTestContext(t, node)
		loc := ctx.packageLocation("//git", node)
		if loc.PackageRoot != filepath.Join(root, "git") {
			t.Fatalf("package root = %q", loc.PackageRoot)
		}
		if loc.SourceTree != filepath.Join(root, "git", "src") {
			t.Fatalf("source tree = %q", loc.SourceTree)
		}
		if loc.TreeRoot != filepath.Join(ctx.Paths.DepsDir, "local", build.MemberDir("git")) {
			t.Fatalf("tree root = %q", loc.TreeRoot)
		}
	})

	t.Run("native member plain", func(t *testing.T) {
		node := locationTestNode("repo/sdk/sub", "repo/sdk", "sub", resolver.SourceKindNative, "/tmp/remote/sub")
		ctx := locationTestContext(t, node)
		loc := ctx.packageLocation("repo/sdk/sub", node)
		wantRoot := filepath.Join(ctx.Paths.DepsDir, "repo", "sdk")
		if loc.OwnerTreeRoot != wantRoot {
			t.Fatalf("owner tree = %q, want %q", loc.OwnerTreeRoot, wantRoot)
		}
		if loc.PackageRoot != filepath.Join(wantRoot, "src", "sub") || loc.SourceTree != loc.PackageRoot {
			t.Fatalf("loc = %+v", loc)
		}
		if loc.TreeRoot != wantRoot {
			t.Fatalf("tree root = %q", loc.TreeRoot)
		}
	})

	t.Run("native member with own SetGit always uses third_party", func(t *testing.T) {
		node := locationTestNode("repo/sdk/third/lvgl", "repo/sdk", "third/lvgl", resolver.SourceKindNative, "/tmp/remote/third/lvgl")
		node.Pkg.SetGit("https://example.invalid/lvgl.git")
		ctx := locationTestContext(t, node)
		loc := ctx.packageLocation("repo/sdk/third/lvgl", node)
		wantTP := filepath.Join(ctx.Paths.DepsDir, "repo", "sdk", "third_party", build.MemberDir("third/lvgl"))
		if loc.ThirdPartyRoot != wantTP || loc.TreeRoot != wantTP || loc.SourceTree != filepath.Join(wantTP, "src") {
			t.Fatalf("own-Git native member loc = %+v, want third_party %q", loc, wantTP)
		}
		if loc.Linked {
			t.Fatalf("unlinked own-Git member must have Linked=false")
		}
	})

	t.Run("linked native container", func(t *testing.T) {
		node := locationTestNode("link/sdk/sub", "link/sdk", "sub", resolver.SourceKindNative, "/tmp/link/sub")
		node.Source.Link = &buildscript.SourceLink{Path: "/tmp/link/sub", Native: true, LocalSource: true}
		ctx := locationTestContext(t, node)
		loc := ctx.packageLocation("link/sdk/sub", node)
		if !loc.Linked || !loc.ContainerLinked || loc.PackageRoot != "/tmp/link/sub" || loc.SourceTree != "/tmp/link/sub" {
			t.Fatalf("loc = %+v", loc)
		}
		if loc.TreeRoot != filepath.Join(ctx.Paths.DepsDir, "link", "sdk") {
			t.Fatalf("tree root = %q", loc.TreeRoot)
		}
	})

	t.Run("registry package", func(t *testing.T) {
		node := locationTestNode("official/tinyexpr", "official", "tinyexpr", resolver.SourceKindRegistry, "/repos/official/packages/t/tinyexpr")
		ctx := locationTestContext(t, node)
		loc := ctx.packageLocation("official/tinyexpr", node)
		wantRoot := filepath.Join(ctx.Paths.DepsDir, "official", "tinyexpr")
		if loc.OwnerTreeRoot != wantRoot || loc.PackageRoot != filepath.Join(wantRoot, "src") {
			t.Fatalf("registry loc = %+v", loc)
		}
	})
}
