package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/buildscript"
)

// The two directory projections come from one source derivation: the
// package-root map keeps the package directory, while workspace dirs follow the
// linked/materialized tree.
func TestSourceTreeProjections(t *testing.T) {
	s := sessionFixture(t)
	linkTarget := filepath.Join(t.TempDir(), "linked-app")
	s.ctx.DepGraph.Packages["app"].Source.Link = &buildscript.SourceLink{Path: linkTarget}

	sources := packageSources(s.ctx)
	app := sources["app"]
	if !app.Linked || app.Tree != linkTarget || app.Root == linkTarget {
		t.Fatalf("app tree = %+v, want linked tree %q over root %q", app, linkTarget, app.Root)
	}
	if got := workspacePackageDirs(s.ctx)["app"]; got != linkTarget {
		t.Fatalf("workspace dir = %q, want %q", got, linkTarget)
	}
	if got := packageSourceDirs(s.ctx)["app"]; got == nil || got.SourceDir != app.Root {
		t.Fatalf("source dirs = %+v, want the package root %q", got, app.Root)
	}
	dep := sources["dep"]
	if dep.Linked || dep.Tree != dep.Root {
		t.Fatalf("dep tree = %+v, want an in-place tree", dep)
	}
}
