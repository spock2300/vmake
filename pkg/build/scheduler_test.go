package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestCollectDepArtifactsMissingNonVoidArtifact(t *testing.T) {
	lib := makeTargetWithDeps("lib")
	lib.SetTest(true)
	app := makeTargetWithDeps("app", "lib")

	graph, err := NewBuildGraph(makeTargets("p", app, lib), nil, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}

	s := &Scheduler{
		graph: graph,
		pkgs:  map[string]*PkgInfo{"p": {PkgDirs: api.PkgDirs{SourceDir: t.TempDir()}}},
	}

	appNode, err := graph.GetNode("p:app")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.collectDepArtifacts(appNode); err == nil || !strings.Contains(err.Error(), "dependency artifact missing") {
		t.Fatalf("collectDepArtifacts err = %v, want missing-artifact error", err)
	}
}

func TestObjectNameIsFlat(t *testing.T) {
	got := objectName(filepath.Join("src", "nested", "foo.c"))
	want := "src_nested_foo.c.o"
	if got != want {
		t.Fatalf("objectName = %q, want %q", got, want)
	}
	if strings.ContainsAny(got, `/\`) {
		t.Fatalf("objectName = %q must not contain a path separator", got)
	}
}
