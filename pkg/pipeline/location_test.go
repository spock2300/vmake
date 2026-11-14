package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
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

func locationTestNode(name string, origin api.SourceOrigin, dir string) *resolver.PackageNode {
	src := buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, origin)
	return resolver.NewPackageNode(name, src, api.NewPackage().SetName(name))
}

func TestPackageLocationMatrix(t *testing.T) {
	root := t.TempDir()

	t.Run("local plain", func(t *testing.T) {
		node := locationTestNode("local/plain", api.SourceLocal, filepath.Join(root, "plain"))
		ctx := locationTestContext(t, node)
		loc := ctx.packageLocation("local/plain", node)
		if loc.PackageRoot != filepath.Join(root, "plain") || loc.SourceTree != loc.PackageRoot {
			t.Fatalf("loc = %+v", loc)
		}
		if loc.TreeRoot != filepath.Join(ctx.Paths.DepsDir, "local", "local/plain") {
			t.Fatalf("tree root = %q", loc.TreeRoot)
		}
	})

	t.Run("local SetGit", func(t *testing.T) {
		node := locationTestNode("local/git", api.SourceLocal, filepath.Join(root, "git"))
		node.Pkg.SetGit("https://example.invalid/git.git")
		ctx := locationTestContext(t, node)
		loc := ctx.packageLocation("local/git", node)
		if loc.PackageRoot != filepath.Join(root, "git") {
			t.Fatalf("package root = %q", loc.PackageRoot)
		}
		if loc.SourceTree != filepath.Join(root, "git", "src") {
			t.Fatalf("source tree = %q", loc.SourceTree)
		}
	})

	t.Run("remote top", func(t *testing.T) {
		node := locationTestNode("repo/native", api.SourceRemote, "/tmp/remote")
		ctx := locationTestContext(t, node)
		loc := ctx.packageLocation("repo/native", node)
		wantRoot := filepath.Join(ctx.Paths.DepsDir, "repo/native")
		if loc.OwnerTreeRoot != wantRoot {
			t.Fatalf("owner tree = %q, want %q", loc.OwnerTreeRoot, wantRoot)
		}
		if loc.PackageRoot != filepath.Join(wantRoot, "src") || loc.SourceTree != loc.PackageRoot {
			t.Fatalf("loc = %+v", loc)
		}
		if loc.TreeRoot != wantRoot {
			t.Fatalf("tree root = %q", loc.TreeRoot)
		}
	})

	t.Run("linked native", func(t *testing.T) {
		node := locationTestNode("link/native", api.SourceRemote, "/tmp/link")
		node.Source.Link = &buildscript.SourceLink{Path: "/tmp/link", Native: true, LocalSource: true}
		ctx := locationTestContext(t, node)
		loc := ctx.packageLocation("link/native", node)
		if !loc.Linked || loc.PackageRoot != "/tmp/link" || loc.SourceTree != "/tmp/link" {
			t.Fatalf("loc = %+v", loc)
		}
		if loc.TreeRoot != filepath.Join(ctx.Paths.DepsDir, "link/native") {
			t.Fatalf("tree root = %q", loc.TreeRoot)
		}
	})
}
