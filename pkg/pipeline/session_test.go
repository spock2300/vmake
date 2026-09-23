package pipeline

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func sessionFixture(t *testing.T) *buildPhaseState {
	t.Helper()
	root := t.TempDir()
	r := resolver.NewResolver(nil, filepath.Join(root, "vmake_deps"))
	r.Graph().Order = []string{"app", "dep"}
	for _, name := range r.Graph().Order {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "build.go"), []byte("package main\n"), 0644); err != nil {
			t.Fatal(err)
		}
		r.Graph().Packages[name] = resolver.NewPackageNode(name, buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, api.SourceLocal), api.NewPackage().SetName(name))
	}
	ctx := &RuntimeContext{Config: emptyConfig(), Resolver: r, DepGraph: r.Graph(), Paths: &Paths{ProjectDir: root, DepsDir: filepath.Join(root, "vmake_deps"), CacheDir: filepath.Join(root, "cache")}}
	tc := testToolchain()
	s := newBuildPhaseState(ctx, BuildOptions{Jobs: 2})
	s.cfg = makeBuildConfig(ctx, tc, tc.Name)
	s.needed = map[string]bool{"app": true, "dep": true}
	s.computeDirsAndOptions()
	s.remote = &remoteVersionState{entries: map[string]*config.EntryConfig{}, commits: map[string]string{}, versionDirs: map[string]string{}}
	return s
}

func TestSessionSubgraphAndMainExecuteOnce(t *testing.T) {
	s := sessionFixture(t)
	var depDeclarations, depBuilds, appBuilds int
	var declared *api.Target
	s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(ctx *api.BuildContext) {
		depDeclarations++
		declared = ctx.Target("generate").SetKind(api.TargetVoid).AddDefines("ORIGINAL").SetBuildFunc(func(*api.Package) error { depBuilds++; return nil })
	})
	s.ctx.DepGraph.Packages["app"].Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.BuildSubGraph("dep")
		ctx.BuildSubGraph("dep")
		declared.Defines()[0] = "MUTATED"
		declared.SetKind(api.TargetBinary)
		ctx.Target("app").SetKind(api.TargetVoid).AddDeps("dep:generate").SetBuildFunc(func(*api.Package) error { appBuilds++; return nil })
	})
	if err := s.executeOnBuild(); err != nil {
		t.Fatal(err)
	}
	result, err := s.buildAndRunPipeline()
	if err != nil {
		t.Fatal(err)
	}
	if depDeclarations != 1 || depBuilds != 1 || appBuilds != 1 {
		t.Fatalf("declarations=%d dep builds=%d app builds=%d", depDeclarations, depBuilds, appBuilds)
	}
	frozen := result.AllTargets["dep"]["generate"]
	if frozen.Kind() != api.TargetVoid || frozen.Defines()[0] != "ORIGINAL" {
		t.Fatalf("target declaration was mutated: %s %v", frozen.Kind(), frozen.Defines())
	}
}

func TestSessionSharesToolProbesAcrossBuildPhases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell compiler wrapper")
	}
	s := sessionFixture(t)
	countFile := filepath.Join(t.TempDir(), "probes")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	for _, tool := range []*string{&s.cfg.Tc.Tools.CC, &s.cfg.Tc.Tools.CXX, &s.cfg.Tc.Tools.AR} {
		path := filepath.Join(s.ctx.Paths.ProjectDir, filepath.Base(*tool))
		body := "#!/bin/sh\nprintf '%s\\n' \"$0 $*\" >> " + quote(countFile) + "\nprintf 'compiler 1.0\\n'\n"
		if err := os.WriteFile(path, []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
		*tool = path
	}
	s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("generate").SetKind(api.TargetVoid)
	})
	s.ctx.DepGraph.Packages["app"].Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.BuildSubGraph("dep")
		ctx.Target("app").SetKind(api.TargetVoid).AddDeps("dep:generate")
	})
	if err := s.prepareAllPackages(); err != nil {
		t.Fatal(err)
	}
	if err := s.executeOnBuild(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.buildAndRunPipeline(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, compiler := range []string{s.cfg.Tc.Tools.CC, s.cfg.Tc.Tools.CXX} {
		if count := strings.Count(string(data), compiler+" --version\n"); count != 1 {
			t.Fatalf("%s probed %d times across preparation, declarations, subgraph and main build: %s", compiler, count, data)
		}
	}
}

func TestSessionRejectsSubgraphInsideTarget(t *testing.T) {
	s := sessionFixture(t)
	s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(ctx *api.BuildContext) { ctx.Target("dep").SetKind(api.TargetVoid) })
	s.ctx.DepGraph.Packages["app"].Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error { ctx.BuildSubGraph("dep"); return nil })
	})
	if err := s.executeOnBuild(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.buildAndRunPipeline(); err == nil || !strings.Contains(err.Error(), "while target app:app is running") {
		t.Fatalf("nested target execution: %v", err)
	}
}

func TestSessionRejectsDeclarationCycle(t *testing.T) {
	s := sessionFixture(t)
	s.ctx.DepGraph.Packages["app"].Pkg.OnBuild(func(ctx *api.BuildContext) { ctx.BuildSubGraph("dep") })
	s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(ctx *api.BuildContext) { ctx.BuildSubGraph("app") })
	if err := s.executeOnBuild(); err == nil || !strings.Contains(err.Error(), "app -> dep -> app") {
		t.Fatalf("declaration cycle: %v", err)
	}
}

func TestSessionSubgraphFailureIsNotRetried(t *testing.T) {
	s := sessionFixture(t)
	failure := errors.New("generation failed")
	var runs int
	s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("dep").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error { runs++; return failure })
	})
	if err := s.executeOnBuild(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.buildSubGraph("dep"); !errors.Is(err, failure) {
			t.Fatalf("build result: %v", err)
		}
	}
	if runs != 1 {
		t.Fatalf("failed target ran %d times", runs)
	}
}

func TestSessionRejectsIncompatiblePackageBinding(t *testing.T) {
	s := sessionFixture(t)
	if err := s.executeOnBuild(); err != nil {
		t.Fatal(err)
	}
	s.scopeValues = maps.Clone(s.cfg.GlobalValues)
	s.scopeValues[api.ModeOptionName] = api.ModeRelease
	if _, err := s.bindPackage("dep"); err == nil || !strings.Contains(err.Error(), "incompatible build configuration") {
		t.Fatalf("configuration binding: %v", err)
	}
}

func registerSessionToolchain(t *testing.T, s *buildPhaseState) *toolchain.Toolchain {
	t.Helper()
	tc := *s.cfg.Tc
	dir := t.TempDir()
	tc.Name = filepath.Base(filepath.Dir(dir)) + "-" + filepath.Base(dir)
	if err := toolchain.GetManager().RegisterToolchain(tc.Name, &tc); err != nil {
		t.Fatal(err)
	}
	return &tc
}

func TestSessionCachedSubgraphValidatesConfiguration(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "success"
		if failed {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			s := sessionFixture(t)
			var failure error
			if failed {
				failure = errors.New("generation failed")
			}
			declarations, builds := 0, 0
			s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(ctx *api.BuildContext) {
				declarations++
				ctx.Target("generate").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error {
					builds++
					return failure
				})
			})
			if err := s.executeOnBuild(); err != nil {
				t.Fatal(err)
			}
			if err := s.buildSubGraph("dep"); !errors.Is(err, failure) {
				t.Fatalf("initial result: %v", err)
			}
			cached := s.subGraphErrors["dep"]
			s.scopeValues = maps.Clone(s.cfg.GlobalValues)
			s.scopeValues[api.ToolchainOptionName] = "uninstalled-conflicting-toolchain"
			scope := maps.Clone(s.scopeValues)
			if err := s.buildSubGraph("dep"); err == nil || !strings.Contains(err.Error(), "package dep was already bound to an incompatible build configuration") {
				t.Fatalf("cached configuration mismatch: %v", err)
			}
			if !reflect.DeepEqual(s.scopeValues, scope) || s.subGraphErrors["dep"] != cached {
				t.Fatal("rejected request mutated scope or cached result")
			}
			s.scopeValues = nil
			if err := s.buildSubGraph("dep"); err != cached {
				t.Fatalf("matching request did not reuse original result: %v, want %v", err, cached)
			}
			if declarations != 1 || builds != 1 {
				t.Fatalf("declarations=%d builds=%d", declarations, builds)
			}
		})
	}
}

func TestSessionCachedSubgraphValidatesDependencyBindings(t *testing.T) {
	for _, dependency := range []string{"require", "target"} {
		t.Run(dependency, func(t *testing.T) {
			s := sessionFixture(t)
			if dependency == "require" {
				s.ctx.DepGraph.Packages["app"].Deps = []string{"dep"}
			}
			builds := 0
			s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(ctx *api.BuildContext) {
				ctx.Target("generate").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error { builds++; return nil })
			})
			s.ctx.DepGraph.Packages["app"].Pkg.OnBuild(func(ctx *api.BuildContext) {
				target := ctx.Target("app").SetKind(api.TargetVoid)
				if dependency == "target" {
					target.AddDeps("dep:generate")
				}
			})
			if err := s.executeOnBuild(); err != nil {
				t.Fatal(err)
			}
			if err := s.buildSubGraph("app"); err != nil {
				t.Fatal(err)
			}
			dirs := *s.pkgDirs["dep"]
			bound := s.bindings["dep"]
			config.SetEntry(s.ctx.Config, "dep", &config.EntryConfig{Options: map[string]any{"enabled": true}})
			if err := s.buildSubGraph("app"); err == nil || !strings.Contains(err.Error(), "package dep was already bound to an incompatible build configuration") {
				t.Fatalf("cached dependency configuration mismatch: %v", err)
			}
			if builds != 1 || s.bindings["dep"] != bound || *s.pkgDirs["dep"] != dirs {
				t.Fatal("configuration validation repeated work or mutated dependency binding")
			}
		})
	}
}

func TestSessionNestedCachedSubgraphToolchainScopes(t *testing.T) {
	for _, override := range []bool{false, true} {
		name := "inherited conflict"
		if override {
			name = "package override"
		}
		t.Run(name, func(t *testing.T) {
			s := sessionFixture(t)
			first, second := registerSessionToolchain(t, s), registerSessionToolchain(t, s)
			if override {
				config.SetEntry(s.ctx.Config, "dep", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: first.Name}})
			}
			for _, outer := range []struct {
				name string
				tc   *toolchain.Toolchain
			}{{"first", first}, {"second", second}} {
				dir := filepath.Join(s.ctx.Paths.ProjectDir, outer.name)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "build.go"), []byte("package main\n"), 0644); err != nil {
					t.Fatal(err)
				}
				pkg := api.NewPackage().SetName(outer.name)
				pkg.OnBuild(func(ctx *api.BuildContext) {
					ctx.BuildSubGraph("dep")
					ctx.Target(outer.name).SetKind(api.TargetVoid)
				})
				s.ctx.DepGraph.Packages[outer.name] = resolver.NewPackageNode(outer.name, buildscript.NewSource(outer.name, filepath.Join(dir, "build.go"), dir, api.SourceLocal), pkg)
				s.needed[outer.name] = true
				config.SetEntry(s.ctx.Config, outer.name, &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: outer.tc.Name}})
			}
			s.ctx.DepGraph.Order = []string{"app", "first", "second", "dep"}
			s.computeDirsAndOptions()
			declarations, builds := 0, 0
			s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(ctx *api.BuildContext) {
				declarations++
				ctx.Target("generate").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error { builds++; return nil })
			})
			s.ctx.DepGraph.Packages["app"].Pkg.OnBuild(func(ctx *api.BuildContext) {
				ctx.BuildSubGraph("first")
				ctx.BuildSubGraph("second")
			})
			err := s.executeOnBuild()
			if override {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.buildAndRunPipeline(); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "package dep was already bound to an incompatible build configuration") {
				t.Fatalf("nested inherited toolchain mismatch: %v", err)
			}
			if declarations != 1 || builds != 1 || s.scopeValues != nil || s.bindings["dep"].toolchain.Name != first.Name {
				t.Fatalf("declarations=%d builds=%d scope=%v dependency toolchain=%s", declarations, builds, s.scopeValues, s.bindings["dep"].toolchain.Name)
			}
		})
	}
}

func TestSessionCachedSubgraphDoesNotRepeatToolchainInstallation(t *testing.T) {
	s := sessionFixture(t)
	tc := registerSessionToolchain(t, s)
	installs, builds := 0, 0
	toolchain.GetManager().SetOnMissing(tc.Name, func(string) (*toolchain.Toolchain, error) {
		installs++
		return tc, nil
	})
	config.SetEntry(s.ctx.Config, "dep", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: tc.Name}})
	s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("generate").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error { builds++; return nil })
	})
	if err := s.executeOnBuild(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.buildSubGraph("dep"); err != nil {
			t.Fatal(err)
		}
	}
	if installs != 1 || builds != 1 {
		t.Fatalf("toolchain installations=%d builds=%d", installs, builds)
	}
}

func TestSessionFailedSubgraphValidatesUnboundRoot(t *testing.T) {
	s := sessionFixture(t)
	s.ctx.DepGraph.Order = []string{"dep", "app"}
	s.pkgMetaMap = map[string]build.PkgBuildMeta{"app": {Deps: []string{"dep"}}, "dep": {}}
	s.allTargets = make(map[string]map[string]*api.Target)
	s.buildCtxs = make(map[string]*api.BuildContext)
	config.SetEntry(s.ctx.Config, "dep", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: s.cfg.TcName}})
	declarations := 0
	s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(*api.BuildContext) {
		declarations++
		panic("dependency declaration failed")
	})
	cached := s.buildSubGraph("app")
	if cached == nil || !strings.Contains(cached.Error(), "dependency declaration failed") || s.bindings["app"] != nil {
		t.Fatalf("initial failure=%v, root binding=%v", cached, s.bindings["app"])
	}
	s.scopeValues = maps.Clone(s.cfg.GlobalValues)
	s.scopeValues[api.ToolchainOptionName] = "other-uninstalled-toolchain"
	if err := s.buildSubGraph("app"); err == nil || !strings.Contains(err.Error(), "subgraph package app was already requested with an incompatible build configuration") {
		t.Fatalf("unbound root configuration mismatch: %v", err)
	}
	s.scopeValues = nil
	if err := s.buildSubGraph("app"); err != cached || declarations != 1 {
		t.Fatalf("matching request retried failed declaration: error=%v, declarations=%d", err, declarations)
	}
}

func TestSessionPackageBindingValuesDoNotMutateConfiguration(t *testing.T) {
	s := sessionFixture(t)
	entry := &config.EntryConfig{}
	config.SetEntry(s.ctx.Config, "dep", entry)
	base := maps.Clone(s.cfg.GlobalValues)
	before := maps.Clone(base)
	values, err := s.packageBindingValues("dep", base)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Options != nil || !reflect.DeepEqual(base, before) || len(s.bindings) != 0 {
		t.Fatal("configuration resolution mutated its inputs or created a binding")
	}
	values[api.ToolchainOptionName] = "changed"
	if !reflect.DeepEqual(base, before) {
		t.Fatal("resolved values alias the inherited scope")
	}
}

func TestSessionRejectsRecursiveSubgraphRequest(t *testing.T) {
	s := sessionFixture(t)
	s.ctx.DepGraph.Packages["app"].Pkg.OnBuild(func(ctx *api.BuildContext) { ctx.BuildSubGraph("dep") })
	s.ctx.DepGraph.Packages["dep"].Pkg.OnBuild(func(ctx *api.BuildContext) { ctx.BuildSubGraph("dep") })
	if err := s.executeOnBuild(); err == nil || !strings.Contains(err.Error(), "recursive BuildSubGraph(dep)") {
		t.Fatalf("recursive subgraph: %v", err)
	}
	if s.scopeValues != nil {
		t.Fatalf("recursive request left scope: %v", s.scopeValues)
	}
}

func TestDeclareTargetsReturnsScriptErrorAndRestoresState(t *testing.T) {
	s := sessionFixture(t)
	pkg := s.ctx.DepGraph.Packages["app"].Pkg
	pkg.OnBuild(func(ctx *api.BuildContext) { ctx.Target("app").AddDeps("invalid:") })
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeclareTargets(s.ctx, "app", s.pkgDirs["app"], s.cfg.Tc, s.cfg.GlobalValues)
	var scriptError *api.BuildScriptError
	if !errors.As(err, &scriptError) {
		t.Fatalf("script error was not returned: %v", err)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if before != after || pkg.DryRun() {
		t.Fatalf("state was not restored: cwd %s -> %s, dry-run %v", before, after, pkg.DryRun())
	}
}
