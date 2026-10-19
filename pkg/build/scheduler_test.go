package build

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
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

func TestCollectDepArtifactsPublicAttributesAndLinkable(t *testing.T) {
	leaf := makeTargetWithDeps("leaf").
		SetKind(api.TargetStatic).
		SetLinkable(false).
		AddPublicIncludes("include").
		AddPublicDefines("LEAF_DEF=1").
		AddPublicCFlags("-Wleaf").
		AddPublicCxxFlags("-std=c++20").
		AddPublicLdFlags("-Wl,--leaf").
		AddPublicLinks("m")
	mid := makeTargetWithDeps("mid", "leaf:leaf").
		SetKind(api.TargetStatic).
		AddPublicDefines("MID_DEF=1")
	app := makeTargetWithDeps("app", "mid:mid", "leaf:leaf").
		SetKind(api.TargetBinary).
		AddPublicDefines("APP_PUBLIC=1")

	targets := map[string]map[string]*api.Target{
		"app":  {"app": app},
		"mid":  {"mid": mid},
		"leaf": {"leaf": leaf},
	}
	graph, err := NewBuildGraph(targets, nil, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}

	leafDir := t.TempDir()
	midDir := t.TempDir()
	appDir := t.TempDir()
	s := newScheduler(schedulerConfig{
		Graph:     graph,
		Toolchain: &toolchain.Toolchain{},
		Tools:     &ResolvedTools{},
		Layouts: map[string]*PackageLayout{
			"leaf": {Dirs: api.PkgDirs{SourceDir: leafDir, BuildDir: leafDir}, BuildKey: "k", Ready: true},
			"mid":  {Dirs: api.PkgDirs{SourceDir: midDir, BuildDir: midDir}, BuildKey: "k", Ready: true},
			"app":  {Dirs: api.PkgDirs{SourceDir: appDir, BuildDir: appDir}, BuildKey: "k", Ready: true},
		},
		GlobalFlags: &[4][]string{},
	})

	wantMid := s.getTargetOutputPath(&BuildNode{PkgName: "mid", Target: mid})
	if err := os.MkdirAll(filepath.Dir(wantMid), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wantMid, []byte("arch"), 0644); err != nil {
		t.Fatal(err)
	}
	wantLeaf := s.getTargetOutputPath(&BuildNode{PkgName: "leaf", Target: leaf})
	if err := os.MkdirAll(filepath.Dir(wantLeaf), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wantLeaf, []byte("obj"), 0644); err != nil {
		t.Fatal(err)
	}

	appNode, err := graph.GetNode("app:app")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := s.resolveTarget(appNode)
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if !reflect.DeepEqual(resolved.DepArtifacts, []string{wantMid}) {
		t.Fatalf("DepArtifacts = %v, want [%s] (leaf is SetLinkable(false))", resolved.DepArtifacts, wantMid)
	}
	if !reflect.DeepEqual(resolved.AllDefines, []string{"NDEBUG", "APP_PUBLIC=1", "MID_DEF=1", "LEAF_DEF=1"}) &&
		!reflect.DeepEqual(resolved.AllDefines, []string{"APP_PUBLIC=1", "MID_DEF=1", "LEAF_DEF=1"}) {
		t.Fatalf("AllDefines = %v", resolved.AllDefines)
	}
	if !reflect.DeepEqual(resolved.AllCFlags, []string{"-O0", "-g", "-Wleaf"}) {
		t.Fatalf("AllCFlags = %v", resolved.AllCFlags)
	}
	if !reflect.DeepEqual(resolved.AllCxxFlags, []string{"-O0", "-g", "-std=c++20"}) {
		t.Fatalf("AllCxxFlags = %v", resolved.AllCxxFlags)
	}
	if !reflect.DeepEqual(resolved.AllLdFlags, []string{"-Wl,--leaf"}) {
		t.Fatalf("AllLdFlags = %v", resolved.AllLdFlags)
	}
	if !reflect.DeepEqual(resolved.AllLinks, []string{"m"}) {
		t.Fatalf("AllLinks = %v", resolved.AllLinks)
	}
}

func TestTargetVoidIncrementalInputsOutputs(t *testing.T) {
	dir := t.TempDir()
	buildDir := filepath.Join(dir, "build")
	inFile := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(inFile, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	runs := 0
	gen := makeTargetWithDeps("gen").
		SetKind(api.TargetVoid).
		AddInputs("in.txt").
		AddOutputs(api.BuildDirToken + "/out.txt").
		SetBuildFunc(func(p *api.Package) error {
			runs++
			data, err := os.ReadFile(filepath.Join(p.SourceDir(), "in.txt"))
			if err != nil {
				return err
			}
			outPath := filepath.Join(p.BuildDir(), "out.txt")
			if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
				return err
			}
			return os.WriteFile(outPath, data, 0644)
		})

	graph, err := NewBuildGraph(makeTargets("p", gen), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pkg := api.NewPackage().SetName("p")
	pkg.SetDirs(api.PkgDirs{SourceDir: dir, BuildDir: buildDir})
	runOnce := func() error {
		s := newScheduler(schedulerConfig{
			Graph:     graph,
			Toolchain: &toolchain.Toolchain{},
			Tools:     &ResolvedTools{},
			Layouts: map[string]*PackageLayout{
				"p": {Dirs: api.PkgDirs{SourceDir: dir, BuildDir: buildDir}, BuildKey: "k", Ready: true},
			},
			Packages:    map[string]*api.Package{"p": pkg},
			GlobalFlags: &[4][]string{},
		})
		return s.Build("p:gen")
	}

	if err := runOnce(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if runs != 1 {
		t.Fatalf("runs = %d, want 1", runs)
	}
	if err := runOnce(); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if runs != 1 {
		t.Fatalf("runs after cached run = %d, want 1", runs)
	}
	if err := os.WriteFile(inFile, []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runOnce(); err != nil {
		t.Fatalf("third run after input edit: %v", err)
	}
	if runs != 2 {
		t.Fatalf("runs after input change = %d, want 2", runs)
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
