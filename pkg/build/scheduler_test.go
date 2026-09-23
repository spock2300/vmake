package build

import (
	"context"
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

	s := &Scheduler{ctx: context.Background(),
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

func TestObjectPathIdentityAndReadableName(t *testing.T) {
	dir := t.TempDir()
	seen := make(map[string]bool)
	for _, test := range []struct{ target, source string }{
		{"p:one", "src/foo.c"},
		{"p:two", "src/foo.c"},
		{"p:one", "other/foo.c"},
		{"p:one", "src/foo.cpp"},
		{"p:one", "src/foo.S"},
	} {
		got, err := objectPath(test.target, test.source, dir)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(filepath.ToSlash(got), "/")
		if len(parts) != 3 || parts[0] != "object" || len(parts[1]) != 64 || parts[2] != "foo.o" {
			t.Fatalf("unexpected object layout: %s", got)
		}
		if seen[got] {
			t.Fatalf("object path collision for %+v: %s", test, got)
		}
		seen[got] = true
		for _, equivalent := range []string{"./" + test.source, filepath.Join(dir, test.source)} {
			other, err := objectPath(test.target, equivalent, dir)
			if err != nil || other != got {
				t.Fatalf("source alias %s changed object path: %s, %v", equivalent, other, err)
			}
		}
	}
}
