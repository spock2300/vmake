package pipeline

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestPackageVisibilityConfigCallbacks(t *testing.T) {
	for _, test := range []struct {
		name    string
		onApply bool
		value   bool
	}{
		{name: "OnConfig", value: true},
		{name: "OnApply enabled", onApply: true, value: true},
		{name: "OnApply disabled", onApply: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			r := resolver.NewResolver(nil, root)
			r.Graph().Order = []string{"dependency", "app"}
			for _, name := range r.Graph().Order {
				r.Graph().Packages[name] = resolver.NewPackageNode(name,
					buildscript.NewSource(name, filepath.Join(root, "build.go"), root, api.SourceLocal),
					api.NewPackage().SetName(name))
			}
			app := r.Graph().Packages["app"].Pkg
			app.OnConfig(func(ctx *api.ConfigContext) {
				if test.onApply {
					ctx.Option("hidden").SetType(api.OptionBool).SetDefault(false).
						SetOnApply(func(ctx *api.ConfigContext, value any) {
							if value.(bool) {
								ctx.SetDefaultVisibilityHidden()
							}
						})
				} else {
					ctx.SetDefaultVisibilityHidden()
				}
			})
			cfg := emptyConfig()
			if test.onApply {
				config.SetEntry(cfg, "app", &config.EntryConfig{Options: map[string]any{"hidden": test.value}})
			}
			ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Config: cfg}
			globalHash := build.GlobalFlagsHash()
			if err := runConfigPhase(ctx); err != nil {
				t.Fatal(err)
			}
			if app.DefaultVisibilityHidden() != test.value {
				t.Errorf("application hidden = %v, want %v", app.DefaultVisibilityHidden(), test.value)
			}
			if r.Graph().Packages["dependency"].Pkg.DefaultVisibilityHidden() {
				t.Error("application policy changed dependency visibility")
			}
			for name, flags := range ctx.BufferedGlobalFlags {
				if !reflect.DeepEqual(flags, &packageGlobalFlags{}) {
					t.Errorf("%s leaked visibility into global flags: %+v", name, flags)
				}
			}
			if got := build.GlobalFlagsHash(); got != globalHash {
				t.Errorf("configuration changed global flags hash: %s -> %s", globalHash, got)
			}
		})
	}
}

func TestPackageVisibilityCacheIsolation(t *testing.T) {
	for _, test := range []struct {
		name   string
		origin api.SourceOrigin
		git    bool
		native bool
	}{
		{name: "local", origin: api.SourceLocal},
		{name: "local SetGit", origin: api.SourceLocal, git: true},
		{name: "registry", origin: api.SourceRemote},
		{name: "native", origin: api.SourceRemote, native: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			app := localNode("app", "dependency")
			dependency := resolver.NewPackageNode("dependency",
				buildscript.NewSource("dependency", filepath.Join(root, "build.go"), root, test.origin),
				api.NewPackage().SetName("dependency"))
			if test.git {
				dependency.Pkg.SetGit("https://example.invalid/dependency.git")
			}
			if test.native {
				dependency.WithNative("https://example.invalid/dependency.git", nil, "1.0")
			}
			r := resolver.NewResolver(nil, root)
			r.Graph().Packages = map[string]*resolver.PackageNode{"app": app, "dependency": dependency}
			s := newBuildPhaseState(&RuntimeContext{Resolver: r, DepGraph: r.Graph()}, BuildOptions{})
			s.needed = map[string]bool{"app": true, "dependency": true}
			s.scriptHashes = map[string]string{"app": "app-script", "dependency": "dependency-script"}
			s.globalFlagsHash = "global-flags"
			s.remote = &remoteVersionState{
				entries: map[string]*config.EntryConfig{"dependency": {Version: "1.0"}},
				commits: map[string]string{"dependency": "commit"},
			}
			before, err := s.buildPkgKeyExtra()
			if err != nil {
				t.Fatal(err)
			}
			api.NewConfigContextWithPackage("app", app.Pkg).SetDefaultVisibilityHidden()
			after, err := s.buildPkgKeyExtra()
			if err != nil {
				t.Fatal(err)
			}
			if before["app"] == after["app"] {
				t.Error("application visibility change reused its cache identity")
			}
			if before["dependency"] != after["dependency"] {
				t.Error("application visibility change invalidated dependency cache")
			}
			if got := packageFlagsHash(s.globalFlagsHash, dependency); got != s.globalFlagsHash {
				t.Errorf("package without hidden changed existing flags hash: %q", got)
			}
			api.NewConfigContextWithPackage("dependency", dependency.Pkg).SetDefaultVisibilityHidden()
			ownPolicy, err := s.buildPkgKeyExtra()
			if err != nil {
				t.Fatal(err)
			}
			if ownPolicy["dependency"] == after["dependency"] || ownPolicy["app"] != after["app"] {
				t.Error("dependency policy must invalidate only its own cache")
			}
			s.globalFlagsHash = "changed-global-flags"
			globalChange, err := s.buildPkgKeyExtra()
			if err != nil {
				t.Fatal(err)
			}
			for name := range s.needed {
				if globalChange[name] == ownPolicy[name] {
					t.Errorf("global flag change reused %s cache identity", name)
				}
			}
		})
	}
}

func TestPackageVisibilityBuildAndInspectKeys(t *testing.T) {
	tc, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	tools, err := build.ResolveTools(tc, api.Platform{})
	if err != nil {
		t.Skipf("host toolchain unavailable: %v", err)
	}
	root := t.TempDir()
	r := resolver.NewResolver(nil, filepath.Join(root, "deps"))
	r.Graph().Order = []string{"dependency", "app"}
	for _, name := range r.Graph().Order {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "build.go")
		if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
			t.Fatal(err)
		}
		r.Graph().Packages[name] = resolver.NewPackageNode(name,
			buildscript.NewSource(name, path, dir, api.SourceLocal), api.NewPackage().SetName(name))
	}
	app := r.Graph().Packages["app"]
	app.Pkg.SetRoot(true)
	app.Deps = []string{"dependency"}
	api.NewConfigContextWithPackage("app", app.Pkg).SetDefaultVisibilityHidden()
	ctx := &RuntimeContext{
		Resolver: r, DepGraph: r.Graph(), Config: emptyConfig(),
		Paths: &Paths{DepsDir: filepath.Join(root, "deps"), CacheDir: filepath.Join(root, "cache")},
	}
	s := newBuildPhaseState(ctx, BuildOptions{})
	s.cfg = makeBuildConfig(ctx, tc, "host")
	s.needed = map[string]bool{"app": true, "dependency": true}
	s.globalFlagsHash = build.GlobalFlagsHash()
	s.computeDirsAndOptions()
	if err := s.prepareAllPackages(); err != nil {
		t.Fatal(err)
	}
	insp, err := Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	extra, err := s.buildPkgKeyExtra()
	if err != nil {
		t.Fatal(err)
	}
	for name := range s.needed {
		if !reflect.DeepEqual(insp.PkgDirs[name], s.pkgDirs[name]) {
			t.Errorf("%s inspection directories differ from build: %+v, %+v", name, insp.PkgDirs[name], s.pkgDirs[name])
		}
		want := build.BuildKey(tools.CCKey(), s.cfg.Mode, s.allPkgOptions[name], extra[name])
		if got := filepath.Base(s.pkgDirs[name].BuildDir); got != want {
			t.Errorf("%s directory key = %s, scheduler key = %s", name, got, want)
		}
	}
}
