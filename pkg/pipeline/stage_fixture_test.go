package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/resolver"
)

// stageFixture drives the real build stages instead of hand-assembling their
// state, so tests exercise the same handoff contracts production does.
type stageFixture struct {
	ctx    *RuntimeContext
	rc     *runContext
	needed NeededSet
	conf   *configStage
	src    *sourceStage
	decl   *declareStage
}

func newStageFixture(t *testing.T, names ...string) *stageFixture {
	t.Helper()
	if len(names) == 0 {
		names = []string{"app", "dep"}
	}
	root := t.TempDir()
	r := resolver.NewResolver(nil, filepath.Join(root, ".vmake_deps"))
	r.Graph().Order = append([]string(nil), names...)
	needed := make(NeededSet, len(names))
	for _, name := range names {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "build.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		src := buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, api.SourceLocal)
		r.Graph().Packages[name] = resolver.NewPackageNode(name, src, api.NewPackage().SetName(name))
		needed[name] = true
	}
	ctx := &RuntimeContext{
		Context:  context.Background(),
		Config:   emptyConfig(),
		Resolver: r,
		DepGraph: r.Graph(),
		Paths: &Paths{
			ProjectDir: root,
			DepsDir:    filepath.Join(root, ".vmake_deps"),
			CacheDir:   filepath.Join(root, "cache"),
			ReposDir:   filepath.Join(root, "repos"),
			LockPath:   filepath.Join(root, ".vmake", "vmake.lock"),
		},
	}
	tc := testToolchain()
	cfg := makeBuildConfig(ctx, tc, tc.Name)
	return &stageFixture{
		ctx:    ctx,
		rc:     newRunContext(ctx, BuildOptions{Jobs: 2}, cfg),
		needed: needed,
	}
}

func (f *stageFixture) PrepareConfig() (*configStage, error) {
	conf, err := prepareConfig(f.rc, f.needed, nil)
	if err != nil {
		return nil, err
	}
	f.conf = conf
	return conf, nil
}

func (f *stageFixture) PrepareSources() (*sourceStage, error) {
	src, err := prepareSources(f.rc, f.conf)
	if err != nil {
		return nil, err
	}
	f.src = src
	return src, nil
}

func (f *stageFixture) Plan() (*planStage, error) {
	return planBuild(f.rc, f.conf, f.src, f.decl)
}

func (f *stageFixture) DeclareTargets() (*declareStage, error) {
	decl, err := declareTargets(f.rc, f.conf, f.src)
	if err != nil {
		return nil, err
	}
	f.decl = decl
	return decl, nil
}

// TestStagesHandoff exercises the prepareConfig -> prepareSources handoff
// through the real stages: the configuration stage owns the flag aggregation
// and the source stage receives a ready layout for every needed package.
func TestStagesHandoff(t *testing.T) {
	skipWithoutHostTools(t)
	f := newStageFixture(t)

	conf, err := f.PrepareConfig()
	if err != nil {
		t.Fatal(err)
	}
	if f.rc.globalFlagsHash == "" {
		t.Fatal("prepareConfig left the global flags hash empty")
	}
	if len(conf.allPkgOptions) != len(f.needed) {
		t.Fatalf("allPkgOptions covers %d packages, want %d", len(conf.allPkgOptions), len(f.needed))
	}
	if conf != f.conf || f.src != nil {
		t.Fatal("prepareConfig did not record the stage on the fixture")
	}

	src, err := f.PrepareSources()
	if err != nil {
		t.Fatal(err)
	}
	for name := range f.needed {
		layout := src.layouts[name]
		if layout == nil {
			t.Fatalf("%s: prepareSources left no layout", name)
		}
		if !layout.Ready || layout.BuildKey == "" || layout.Dirs.BuildDir == "" {
			t.Fatalf("%s: layout = %+v, want a ready build key", name, layout)
		}
		dirs := src.pkgDirs[name]
		if dirs == nil || dirs.SourceDir != layout.Dirs.SourceDir {
			t.Fatalf("%s: pkgDirs = %+v, layout dirs = %+v", name, dirs, layout.Dirs)
		}
	}
}

// TestStagesDeclareTargets drives the declare stage through the fixture: the
// OnBuild callback runs once and its targets reach the plan stage's input.
func TestStagesDeclareTargets(t *testing.T) {
	skipWithoutHostTools(t)
	f := newStageFixture(t, "app")
	declarations := 0
	f.ctx.DepGraph.Packages["app"].Pkg.SetRoot(true).OnBuild(func(ctx *api.BuildContext) {
		declarations++
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
	if _, err := f.PrepareConfig(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.PrepareSources(); err != nil {
		t.Fatal(err)
	}
	decl, err := f.DeclareTargets()
	if err != nil {
		t.Fatal(err)
	}
	if declarations != 1 {
		t.Fatalf("OnBuild ran %d times, want 1", declarations)
	}
	if decl.allTargets["app"]["app"] == nil {
		t.Fatalf("allTargets = %+v, want app:app", decl.allTargets)
	}
	if _, ok := decl.pkgMetaMap["app"]; !ok {
		t.Fatalf("pkgMetaMap = %+v, want an entry for app", decl.pkgMetaMap)
	}

	plan, err := f.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || plan.graph == nil || plan.job == nil {
		t.Fatalf("plan = %+v, want a graph and a build job", plan)
	}
	if _, err := plan.graph.GetNode("app:app"); err != nil {
		t.Fatalf("plan graph does not contain app:app: %v", err)
	}
	if plan.job.Env == nil || plan.job.Env.Layouts["app"] == nil {
		t.Fatalf("plan environment lost the prepared layouts: %+v", plan.job.Env)
	}
}
