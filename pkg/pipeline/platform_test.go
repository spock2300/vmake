package pipeline

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestBuildConfigEffectiveModeAndToolchain(t *testing.T) {
	for _, test := range []struct {
		name           string
		configuredMode string
		configuredTC   string
		modeOverride   string
		toolchainName  string
		wantMode       string
		declareGlobals bool
	}{
		{name: "defaults", toolchainName: "host", wantMode: api.ModeDebug},
		{name: "configured debug", configuredMode: api.ModeDebug, configuredTC: "configured", toolchainName: "configured", wantMode: api.ModeDebug},
		{name: "configured release", configuredMode: api.ModeRelease, configuredTC: "configured", toolchainName: "configured", wantMode: api.ModeRelease},
		{name: "flags without config", modeOverride: api.ModeDebug, toolchainName: "selected", wantMode: api.ModeDebug},
		{name: "flags override config", configuredMode: api.ModeRelease, configuredTC: "configured", modeOverride: api.ModeDebug, toolchainName: "selected", wantMode: api.ModeDebug},
		{name: "declared global defaults", toolchainName: "host", wantMode: api.ModeDebug, declareGlobals: true},
		{name: "declared globals with flags", configuredMode: api.ModeRelease, configuredTC: "configured", modeOverride: api.ModeDebug, toolchainName: "selected", wantMode: api.ModeDebug, declareGlobals: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			node := localNode("app")
			options := api.NewConfigContext("app")
			if test.declareGlobals {
				options.GlobalMode()
				options.GlobalOption(api.ToolchainOptionName).SetType(api.OptionChoice).SetDefault("first").SetValues("first", "host", "configured", "selected")
			}
			ctx := &RuntimeContext{
				Config:       emptyConfig(),
				DepGraph:     &resolver.Graph{Packages: map[string]*resolver.PackageNode{"app": node}},
				AllOptions:   map[string]map[string]*api.Option{"app": options.GetOptions()},
				ModeOverride: test.modeOverride,
			}
			ctx.Config.Global.Mode = test.configuredMode
			ctx.Config.Global.Toolchain = test.configuredTC
			var err error
			ctx.GlobalOptions, err = api.MergeGlobalOptions(ctx.AllOptions, []string{"first", "host", "configured", "selected"})
			if err != nil {
				t.Fatal(err)
			}
			tc := testToolchain()
			tc.Name = test.toolchainName
			cfg := makeBuildConfig(ctx, tc, tc.Name)
			if cfg.Mode != test.wantMode {
				t.Errorf("build mode = %q, want %q", cfg.Mode, test.wantMode)
			}
			node.Pkg.OnBuild(func(buildCtx *api.BuildContext) {
				for name, want := range map[string]string{api.ModeOptionName: test.wantMode, api.ToolchainOptionName: test.toolchainName} {
					if got := buildCtx.String(name); got != want {
						t.Errorf("BuildContext %s = %q, want %q", name, got, want)
					}
					if got := node.Pkg.String(name); got != want {
						t.Errorf("Package %s = %q, want %q", name, got, want)
					}
				}
			})
			dir := t.TempDir()
			if _, err := DeclareTargets(ctx, "app", &api.PkgDirs{SourceDir: dir, BuildDir: filepath.Join(dir, "build")}, tc, cfg.GlobalValues); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProjectGlobalValuesPreserveExplicitValues(t *testing.T) {
	options := api.NewConfigContext("app")
	options.GlobalOption("text").SetType(api.OptionString).SetDefault("default")
	options.GlobalOption("enabled").SetType(api.OptionBool).SetDefault(true)
	options.GlobalOption("count").SetType(api.OptionInt).SetDefault(7)
	options.GlobalOption("unset").SetType(api.OptionString).SetDefault("default")
	ctx := &RuntimeContext{Config: emptyConfig(), GlobalOptions: options.GetOptions()}
	ctx.Config.Global.Options = map[string]any{"text": "", "enabled": false, "count": 0, "unset": nil}
	want := map[string]any{"text": "", "enabled": false, "count": 0, "unset": "default"}
	if got := projectGlobalValues(ctx); !reflect.DeepEqual(got, want) {
		t.Fatalf("global values = %#v, want %#v", got, want)
	}
}

func TestProjectPlatformDefaultsAndOverrides(t *testing.T) {
	for _, test := range []struct {
		name       string
		values     map[string]any
		wantOS     string
		wantTriple string
	}{
		{name: "defaults", wantOS: "none", wantTriple: "arm-none-eabi"},
		{name: "explicit empty", values: map[string]any{api.TargetOSOptionName: "", api.TargetTripleOptionName: ""}, wantOS: runtime.GOOS},
		{name: "explicit host", values: map[string]any{api.TargetOSOptionName: "host", api.TargetTripleOptionName: ""}, wantOS: runtime.GOOS},
		{name: "nil keeps defaults", values: map[string]any{api.TargetOSOptionName: nil, api.TargetTripleOptionName: nil}, wantOS: "none", wantTriple: "arm-none-eabi"},
	} {
		t.Run(test.name, func(t *testing.T) {
			node := localNode("app")
			options := api.NewConfigContext("app")
			options.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")
			options.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString).SetDefault("arm-none-eabi")
			ctx := &RuntimeContext{
				Config:        emptyConfig(),
				DepGraph:      &resolver.Graph{Packages: map[string]*resolver.PackageNode{"app": node}},
				AllOptions:    map[string]map[string]*api.Option{"app": options.GetOptions()},
				GlobalOptions: options.GetOptions(),
			}
			ctx.Config.Global.Options = test.values
			platform, err := ProjectPlatform(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := api.Platform{OS: test.wantOS, Triple: test.wantTriple}
			if platform != want {
				t.Errorf("project platform = %+v, want %+v", platform, want)
			}
			tc := testToolchain()
			cfg := makeBuildConfig(ctx, tc, tc.Name)
			node.Pkg.OnBuild(func(buildCtx *api.BuildContext) {
				if got := node.Pkg.TargetOS(); got != test.wantOS {
					t.Errorf("Package.TargetOS = %q, want %q", got, test.wantOS)
				}
				if got := node.Pkg.TargetTriple(); got != test.wantTriple {
					t.Errorf("Package.TargetTriple = %q, want %q", got, test.wantTriple)
				}
				if got := buildCtx.String(api.TargetTripleOptionName); got != test.wantTriple {
					t.Errorf("BuildContext target triple = %q, want %q", got, test.wantTriple)
				}
				if got := node.Pkg.String(api.TargetTripleOptionName); got != test.wantTriple {
					t.Errorf("Package target triple option = %q, want %q", got, test.wantTriple)
				}
			})
			dir := t.TempDir()
			if _, err := DeclareTargets(ctx, "app", &api.PkgDirs{SourceDir: dir, BuildDir: filepath.Join(dir, "build")}, tc, cfg.GlobalValues); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestResolvedGlobalValuesPreservePackageOverrides(t *testing.T) {
	node := localNode("app")
	options := api.NewConfigContext("app")
	options.GlobalOption("global").SetType(api.OptionString).SetDefault("default")
	options.Option("local").SetType(api.OptionString).SetDefault("local default")
	ctx := &RuntimeContext{Config: emptyConfig(), AllOptions: map[string]map[string]*api.Option{"app": options.GetOptions()}}
	values := mergeCfgVals("app", node, ctx, map[string]any{"global": "resolved", "local": "global default"}, map[string]map[string]any{"app": {"global": "package override"}})
	if got := values["global"]; got != "package override" {
		t.Errorf("explicit package value = %v, want package override", got)
	}
	if got := values["local"]; got != "local default" {
		t.Errorf("local value = %v, want local default", got)
	}
}
