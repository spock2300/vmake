package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func newTestScheduler(t *testing.T, targets map[string]map[string]*api.Target) (*Scheduler, *BuildGraph) {
	t.Helper()
	graph, err := NewBuildGraph(targets, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	tools := &ResolvedTools{CC: "cc", CXX: "c++", AR: "ar"}
	s := &Scheduler{ctx: context.Background(),
		graph: graph, pkgs: map[string]*PkgInfo{}, packages: map[string]*api.Package{},
		compiler: NewCompiler(tools), linker: NewLinker(tools),
		ccWriter: NewCompileCommandsWriter(tools), rootDir: root,
	}
	for pkgName := range targets {
		dir := filepath.Join(root, pkgName)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		s.pkgs[pkgName] = &PkgInfo{PkgDirs: api.PkgDirs{SourceDir: dir, BuildDir: filepath.Join(dir, "build")}, OutputDir: filepath.Join(dir, "build")}
	}
	return s, graph
}

func TestBuildAllSerialInterleavedPackages(t *testing.T) {
	targets := map[string]map[string]*api.Target{
		"a": {"a0": makeTargetWithDeps("a0").SetKind(api.TargetVoid), "a1": makeTargetWithDeps("a1", "b:b0").SetKind(api.TargetVoid)},
		"b": {"b0": makeTargetWithDeps("b0").SetKind(api.TargetVoid), "b1": makeTargetWithDeps("b1", "a:a0").SetKind(api.TargetVoid)},
	}
	s, graph := newTestScheduler(t, targets)
	var built []string
	s.SetBuildTargetFunc(func(name string) error {
		built = append(built, name)
		return s.Build(name)
	})
	if err := s.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(built, graph.Order) {
		t.Fatalf("built %v, want target order %v", built, graph.Order)
	}
}

func TestBuildAllFailureCascade(t *testing.T) {
	for _, keepGoing := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "keep-going"}[keepGoing], func(t *testing.T) {
			s, _ := newTestScheduler(t, makeTargets("p",
				makeTargetWithDeps("a").SetKind(api.TargetVoid),
				makeTargetWithDeps("b", "a").SetKind(api.TargetVoid),
				makeTargetWithDeps("z").SetKind(api.TargetVoid)))
			s.SetKeepGoing(keepGoing)
			var built []string
			s.SetBuildTargetFunc(func(name string) error {
				built = append(built, name)
				if name == "p:a" {
					return errors.New("compile failed")
				}
				return nil
			})
			if err := s.BuildAll(); err == nil {
				t.Fatal("expected failure")
			}
			want := []string{"p:a"}
			if keepGoing {
				want = append(want, "p:z")
			}
			if !slices.Equal(built, want) {
				t.Fatalf("built %v, want %v", built, want)
			}
		})
	}
}

func TestVoidBuildIgnoresOldStampsAndPartialInstall(t *testing.T) {
	count := 0
	target := makeTargetWithDeps("external").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error {
		count++
		return nil
	})
	s, _ := newTestScheduler(t, makeTargets("p", target))
	dirs := s.pkgs["p"].PkgDirs
	dirs.InstallDir = filepath.Join(dirs.BuildDir, "install")
	for _, path := range []string{filepath.Join(dirs.BuildDir, ".vmake_stamp"), filepath.Join(dirs.InstallDir, "partial")} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("old state"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s.SetPackage("p", api.NewPackage().SetDirs(dirs))
	for i := 0; i < 2; i++ {
		if err := s.Build("p:external"); err != nil {
			t.Fatal(err)
		}
	}
	if count != 2 {
		t.Fatalf("external callback ran %d times, want 2", count)
	}
}

func TestPkgInfoOfMissingPkg(t *testing.T) {
	s := &Scheduler{ctx: context.Background(), pkgs: map[string]*PkgInfo{}}
	if _, err := s.pkgInfoOf("missing"); err == nil {
		t.Fatal("missing package dirs accepted")
	}
}

func saveTestLinkRecord(t *testing.T, scheduler *Scheduler, resolved *ResolvedTarget, objects []string) {
	t.Helper()
	action, err := scheduler.planLink(resolved, objects)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveActionRecord(action.recordPath, action.signature, scheduler.pkgs[resolved.Node.PkgName].SourceDir, action.inputs, action.outputs); err != nil {
		t.Fatal(err)
	}
}
