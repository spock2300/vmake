package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/buildscript"
)

func TestSourceTreeProjections(t *testing.T) {
	s := sessionFixture(t)
	linkTarget := filepath.Join(t.TempDir(), "linked-app")
	s.ctx.DepGraph.Packages["//app"].Pkg.SetGit("https://example.invalid/app.git")
	s.ctx.DepGraph.Packages["//app"].Source.Link = &buildscript.SourceLink{Path: linkTarget, LocalSource: true}

	sources := NewSourceWorkspace(s.ctx).sources()
	app := sources["//app"]
	if !app.Linked() || app.SourceTree() != linkTarget || app.BuildRoot() == linkTarget {
		t.Fatalf("app tree = %+v, want linked tree %q over root %q", app, linkTarget, app.BuildRoot())
	}
	if got := workspacePackageDirs(s.ctx)["//app"]; got != linkTarget {
		t.Fatalf("workspace dir = %q, want %q", got, linkTarget)
	}
	if got := packageSourceDirs(s.ctx)["//app"]; got == nil || got.SourceDir != app.BuildRoot() {
		t.Fatalf("source dirs = %+v, want the package root %q", got, app.BuildRoot())
	}
	dep := sources["//dep"]
	if dep.Linked() || dep.SourceTree() != dep.BuildRoot() {
		t.Fatalf("dep tree = %+v, want an in-place tree", dep)
	}
}
