package pipeline

import (
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

func TestPackagePlatformBeforeTargetDeclaration(t *testing.T) {
	for _, test := range []struct {
		name       string
		global     map[string]any
		local      map[string]any
		explicit   map[string]any
		wantOS     string
		wantTriple string
	}{
		{name: "global default", wantOS: "none", wantTriple: "arm-none-eabi"},
		{name: "global explicit", global: map[string]any{"target_os": "windows", "target_triple": "mingw"}, wantOS: "windows", wantTriple: "mingw"},
		{name: "global empty", global: map[string]any{"target_os": "", "target_triple": ""}, wantOS: runtime.GOOS},
		{name: "global host", global: map[string]any{"target_os": "host", "target_triple": ""}, wantOS: runtime.GOOS},
		{name: "local defaults", local: map[string]any{"target_os": "windows", "target_triple": "mingw"}, wantOS: "windows", wantTriple: "mingw"},
		{name: "local defaults override global values", global: map[string]any{"target_os": "linux", "target_triple": "aarch64-linux-gnu"}, local: map[string]any{"target_os": "windows", "target_triple": "mingw"}, wantOS: "windows", wantTriple: "mingw"},
		{name: "package explicit", explicit: map[string]any{"target_os": "windows", "target_triple": "mingw"}, wantOS: "windows", wantTriple: "mingw"},
		{name: "package explicit overrides local defaults", local: map[string]any{"target_os": "windows", "target_triple": "mingw"}, explicit: map[string]any{"target_os": "linux", "target_triple": "aarch64-linux-gnu"}, wantOS: "linux", wantTriple: "aarch64-linux-gnu"},
		{name: "package explicit empty", explicit: map[string]any{"target_os": "", "target_triple": ""}, wantOS: runtime.GOOS},
		{name: "package local empty", local: map[string]any{"target_os": "", "target_triple": ""}, wantOS: runtime.GOOS},
		{name: "package nil inherits", explicit: map[string]any{"target_os": nil, "target_triple": nil}, wantOS: "none", wantTriple: "arm-none-eabi"},
	} {
		for _, dryRun := range []bool{false, true} {
			name := test.name + "/build"
			if dryRun {
				name = test.name + "/inspect"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				r := resolver.NewResolver(nil, root)
				node := localNode("app")
				node.Source.Dir = root
				r.Graph().Packages["app"] = node
				r.Graph().Order = []string{"app"}
				globals := api.NewConfigContext("root")
				globals.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")
				globals.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString).SetDefault("arm-none-eabi")
				locals := api.NewConfigContext("app")
				for key, value := range test.local {
					locals.Option(key).SetType(api.OptionString).SetDefault(value)
				}
				ctx := &RuntimeContext{Config: emptyConfig(), Resolver: r, DepGraph: r.Graph(), GlobalOptions: globals.GetOptions(), AllOptions: map[string]map[string]*api.Option{"app": locals.GetOptions()}}
				ctx.Paths = &Paths{ProjectDir: root, DepsDir: filepath.Join(root, "vmake_deps"), CacheDir: filepath.Join(root, "cache")}
				ctx.Config.Global.Options = test.global
				config.SetEntry(ctx.Config, "app", &config.EntryConfig{Options: test.explicit})
				tc := testToolchain()
				tc.Name = "host"
				cfg := makeBuildConfig(ctx, tc, tc.Name)
				node.Pkg.OnBuild(func(buildCtx *api.BuildContext) {
					if node.Pkg.TargetOS() != test.wantOS || node.Pkg.TargetTriple() != test.wantTriple {
						t.Errorf("Package platform = %s/%s, want %s/%s", node.Pkg.TargetOS(), node.Pkg.TargetTriple(), test.wantOS, test.wantTriple)
					}
					ctxOS := platformFromValues(map[string]any{api.TargetOSOptionName: buildCtx.String(api.TargetOSOptionName)}).OS
					if ctxOS != test.wantOS || buildCtx.String(api.TargetTripleOptionName) != test.wantTriple {
						t.Errorf("BuildContext platform = %s/%s, want %s/%s", ctxOS, buildCtx.String(api.TargetTripleOptionName), test.wantOS, test.wantTriple)
					}
					target := buildCtx.Target("app").SetKind(api.TargetBinary)
					wantFlags := api.DefaultBuildFlags("host", test.wantOS)
					if !reflect.DeepEqual(target.CFlags(), wantFlags.CFlags) || !reflect.DeepEqual(target.LdFlags(), wantFlags.LdFlags) {
						t.Errorf("target flags = %v / %v, want %v / %v", target.CFlags(), target.LdFlags(), wantFlags.CFlags, wantFlags.LdFlags)
					}
				})
				dirs := &api.PkgDirs{SourceDir: root, BuildDir: filepath.Join(root, "build")}
				if dryRun {
					if _, err := DeclareTargets(ctx, "app", dirs, tc, cfg.GlobalValues); err != nil {
						t.Fatal(err)
					}
				} else {
					s := newBuildPhaseState(ctx, BuildOptions{})
					s.cfg = cfg
					s.pkgDirs = map[string]*api.PkgDirs{"app": dirs}
					s.allPkgOptions = map[string]map[string]any{"app": test.explicit}
					s.allTargets = make(map[string]map[string]*api.Target)
					s.buildCtxs = make(map[string]*api.BuildContext)
					if err := s.executeOnePackage("app", node); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestPackagePlatformRawValuesAndInputIsolation(t *testing.T) {
	for _, rawOS := range []string{"", "host"} {
		ctx := &RuntimeContext{Config: emptyConfig()}
		ctx.Config.Global.Options = map[string]any{api.TargetOSOptionName: rawOS, api.TargetTripleOptionName: ""}
		config.SetEntry(ctx.Config, "app", &config.EntryConfig{Options: map[string]any{"enabled": true}})
		values, err := PackageConfigValues(ctx, "app", projectGlobalValues(ctx))
		if err != nil {
			t.Fatal(err)
		}
		if values[api.TargetOSOptionName] != rawOS || values[api.TargetTripleOptionName] != "" {
			t.Errorf("raw platform values = %#v", values)
		}
		values["enabled"] = false
		if got := ctx.Config.Entries["app"].Options; !reflect.DeepEqual(got, map[string]any{"enabled": true}) {
			t.Errorf("configuration was mutated: %#v", got)
		}
		platform, err := PackagePlatform(ctx, "app")
		if err != nil || platform != (api.Platform{OS: runtime.GOOS}) {
			t.Errorf("normalized platform = %+v, %v", platform, err)
		}
	}
}

func TestPackagePlatformRejectsInvalidValues(t *testing.T) {
	for _, key := range []string{api.TargetOSOptionName, api.TargetTripleOptionName} {
		ctx := &RuntimeContext{Config: emptyConfig()}
		config.SetEntry(ctx.Config, "app", &config.EntryConfig{Options: map[string]any{key: false}})
		if _, err := PackagePlatform(ctx, "app"); err == nil || !strings.Contains(err.Error(), "package app option "+key) {
			t.Errorf("invalid %s platform: %v", key, err)
		}
		if _, err := PackageConfigValues(ctx, "app", nil); err == nil || !strings.Contains(err.Error(), "package app option "+key) {
			t.Errorf("invalid %s config values: %v", key, err)
		}
	}
}

func TestPackagePlatformBuildAndInspectKeys(t *testing.T) {
	tc, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(tc, api.Platform{}); err != nil {
		t.Skipf("host toolchain unavailable: %v", err)
	}
	root := t.TempDir()
	r := resolver.NewResolver(nil, root)
	path := filepath.Join(root, "build.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	node := resolver.NewPackageNode("app", buildscript.NewSource("app", path, root, api.SourceLocal), api.NewPackage().SetName("app").SetRoot(true))
	r.Graph().Packages["app"] = node
	r.Graph().Order = []string{"app"}
	options := api.NewConfigContext("app")
	localOS := options.Option(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")
	ctx := &RuntimeContext{
		Resolver: r, DepGraph: r.Graph(), Config: emptyConfig(),
		AllOptions: map[string]map[string]*api.Option{"app": options.GetOptions()},
		Paths:      &Paths{DepsDir: filepath.Join(root, "deps"), CacheDir: filepath.Join(root, "cache")},
	}
	var previousDir string
	for _, targetOS := range []string{"none", "windows"} {
		localOS.SetDefault(targetOS)
		s := newBuildPhaseState(ctx, BuildOptions{})
		s.cfg = makeBuildConfig(ctx, tc, tc.Name)
		s.needed = map[string]bool{"app": true}
		s.globalFlagsHash = build.GlobalFlagsHash()
		s.computeDirsAndOptions()
		if err := s.prepareAllPackages(); err != nil {
			t.Fatal(err)
		}
		inspection, err := Inspect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(inspection.PkgDirs["app"], s.pkgDirs["app"]) {
			t.Errorf("%s inspection/build dirs differ: %+v, %+v", targetOS, inspection.PkgDirs["app"], s.pkgDirs["app"])
		}
		if s.pkgDirs["app"].BuildDir == previousDir {
			t.Error("package platform change reused its previous build directory")
		}
		previousDir = s.pkgDirs["app"].BuildDir
	}
}

func TestInspectUsesUnreachablePackagePlatform(t *testing.T) {
	tc, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	tools, err := build.ResolveTools(tc, api.Platform{OS: "none", Triple: "arm-none-eabi"})
	if err != nil {
		t.Skipf("host toolchain unavailable: %v", err)
	}
	root := t.TempDir()
	r := resolver.NewResolver(nil, filepath.Join(root, "deps"))
	for _, name := range []string{"unused", "app"} {
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
	r.Graph().Packages["app"].Pkg.SetRoot(true)
	r.Graph().Order = []string{"unused", "app"}
	options := api.NewConfigContext("unused")
	options.Option(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")
	options.Option(api.TargetTripleOptionName).SetType(api.OptionString).SetDefault("arm-none-eabi")
	ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Config: emptyConfig(),
		AllOptions: map[string]map[string]*api.Option{"unused": options.GetOptions()},
	}
	inspection, err := Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Needed["unused"] {
		t.Fatal("unused package unexpectedly reachable from app")
	}
	node := r.Graph().Packages["unused"]
	scriptHash, err := scriptHashForNode("unused", node)
	if err != nil {
		t.Fatal(err)
	}
	want := makeLocalPkgDirs(node.Source.Dir, tools.CCKey(), inspection.Mode, config.GetEntry(ctx.Config, "unused").Options, inspection.GlobalFlagsHash, scriptHash)
	if got := inspection.PkgDirs["unused"]; !reflect.DeepEqual(got, want) {
		t.Errorf("unreachable package directories = %+v, want its platform directories %+v", got, want)
	}
}
