package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestDisplayPackageValuesProjection(t *testing.T) {
	node := localNode("app")
	options := api.NewConfigContext("app")
	options.GlobalOption("board").SetType(api.OptionChoice).SetDefault("evb").SetValues("evb", "custom")
	options.Option("count").SetType(api.OptionInt).SetDefault(3)
	options.Option("label").SetType(api.OptionString).SetDefault("default")
	allOptions := map[string]map[string]*api.Option{"app": options.GetOptions()}

	cfg := emptyConfig()
	cfg.Global.Options = map[string]any{"board": "custom"}
	config.SetEntry(cfg, "app", &config.EntryConfig{Options: map[string]any{
		"label": "explicit",
		"board": "evb",
	}})
	ctx := &RuntimeContext{
		Config:     cfg,
		DepGraph:   &resolver.Graph{Packages: map[string]*resolver.PackageNode{"app": node}},
		AllOptions: allOptions,
	}
	globalOptions, err := api.MergeGlobalOptions(allOptions, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx.globalOptions = globalOptions
	ctx.derivedOptions = map[string]map[string]any{"app": {"derived": "yes"}}

	values := DisplayPackageValues(ctx, "app", ProjectGlobalValues(ctx))
	for name, want := range map[string]any{
		"count": 3,
		"label": "explicit",
		"board": "custom",
	} {
		if got := values[name]; got != want {
			t.Errorf("%s = %#v, want %#v", name, got, want)
		}
	}
	for _, name := range []string{api.TargetOSOptionName, api.TargetTripleOptionName, "derived", api.ModeOptionName} {
		if _, exists := values[name]; exists {
			t.Errorf("display projection included non-declared %s", name)
		}
	}
}

func TestApplyGlobalValuesUsesChipTargetDefaults(t *testing.T) {
	ctx := &RuntimeContext{
		Config:      emptyConfig(),
		chipProfile: api.ChipProfile{Name: "chip", TargetOS: "none", TargetTriple: "arm-none-eabi"},
	}
	values := applyGlobalValues(ctx, nil)
	if got := values[api.ModeOptionName]; got != api.ModeDebug {
		t.Errorf("mode = %v, want %s", got, api.ModeDebug)
	}
	if got := values[api.TargetOSOptionName]; got != "none" {
		t.Errorf("target_os = %v, want none", got)
	}
	if got := values[api.TargetTripleOptionName]; got != "arm-none-eabi" {
		t.Errorf("target_triple = %v, want arm-none-eabi", got)
	}
}

func TestOnApplySeesResolvedValues(t *testing.T) {
	root := t.TempDir()
	r := resolver.NewResolver(nil, root)
	r.Graph().Order = []string{"app"}
	node := resolver.NewPackageNode("app",
		buildscript.NewSource("app", filepath.Join(root, "build.go"), root, api.SourceLocal),
		api.NewPackage().SetName("app"))
	r.Graph().Packages["app"] = node

	var seenMode, seenToolchain, seenOS, seenTriple string
	node.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("probe").SetType(api.OptionString).SetDefault("").
			SetOnApply(func(applyCtx *api.ConfigContext, _ any) {
				seenMode = applyCtx.String(api.ModeOptionName)
				seenToolchain = applyCtx.String(api.ToolchainOptionName)
				seenOS = applyCtx.String(api.TargetOSOptionName)
				seenTriple = applyCtx.String(api.TargetTripleOptionName)
			})
	})

	cfg := emptyConfig()
	ctx := &RuntimeContext{
		Resolver:    r,
		DepGraph:    r.Graph(),
		Config:      cfg,
		chipProfile: api.ChipProfile{Name: "chip", TargetOS: "none", TargetTriple: "arm-none-eabi"},
	}
	if err := runConfigPhase(ctx); err != nil {
		t.Fatal(err)
	}
	if seenMode != api.ModeDebug {
		t.Errorf("OnApply mode = %q, want %q", seenMode, api.ModeDebug)
	}
	if want := ResolveToolchainName(cfg, "", ""); seenToolchain != want {
		t.Errorf("OnApply toolchain = %q, want %q", seenToolchain, want)
	}
	if seenOS != "none" {
		t.Errorf("OnApply target_os = %q, want none", seenOS)
	}
	if seenTriple != "arm-none-eabi" {
		t.Errorf("OnApply target_triple = %q, want arm-none-eabi", seenTriple)
	}
}
