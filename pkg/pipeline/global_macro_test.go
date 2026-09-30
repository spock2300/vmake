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

func singleChipGraph(t *testing.T) (*resolver.Resolver, *api.Package) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "chip")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	r := resolver.NewResolver(nil, root)
	r.Graph().Order = []string{"chip"}
	r.Graph().Packages["chip"] = resolver.NewPackageNode("chip",
		buildscript.NewSource("chip", filepath.Join(dir, "build.go"), dir, api.SourceLocal),
		api.NewPackage().SetName("chip"))
	return r, r.Graph().Packages["chip"].Pkg
}

func TestGlobalOptionsExportMacros(t *testing.T) {
	r, chip := singleChipGraph(t)
	chip.OnConfig(func(ctx *api.ConfigContext) {
		ctx.GlobalOption("cpu_clock_hz").SetType(api.OptionInt).SetDefault(416000000)
		ctx.GlobalOption("mcu").SetType(api.OptionChoice).
			SetDefault("py32f539").SetValues("py32f539")
		ctx.GlobalOption("variant").SetType(api.OptionChoice).
			SetDefault("M").SetValues("G", "L", "M").
			SetMacroName("PY32F539xx%s")
		ctx.GlobalMode()
	})
	cfg := emptyConfig()
	cfg.Global = &config.GlobalConfig{Options: map[string]any{
		"cpu_clock_hz": 208000000,
		"variant":      "L",
	}}
	ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Config: cfg}
	if err := runConfigPhase(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"CONFIG_CPU_CLOCK_HZ=208000000",
		`CONFIG_MCU="py32f539"`,
		"CONFIG_MCU_PY32F539=1",
		"PY32F539xxL=1",
	}
	if !reflect.DeepEqual(ctx.GlobalMacroDefines, want) {
		t.Fatalf("defines = %v, want %v", ctx.GlobalMacroDefines, want)
	}

	mgr := toolchain.GetManager()
	mgr.SetProjectFlags(nil, nil, nil, nil)
	defer mgr.SetProjectFlags(nil, nil, nil, nil)
	emptyHash := build.GlobalFlagsHash()
	applyGlobalFlagsFromNeeded(ctx, map[string]bool{"chip": true})
	flags := mgr.GetGlobalCFlags()
	for _, define := range want {
		if !contains(flags, "-D"+define) {
			t.Errorf("global C flags missing -D%s: %v", define, flags)
		}
	}
	cxxFlags := mgr.GetGlobalCxxFlags()
	for _, define := range want {
		if !contains(cxxFlags, "-D"+define) {
			t.Errorf("global C++ flags missing -D%s: %v", define, cxxFlags)
		}
	}
	if build.GlobalFlagsHash() == emptyHash {
		t.Error("exported defines must change the global flags hash")
	}
}

func TestGlobalOptionOnApplyUsesConfiguredValue(t *testing.T) {
	r, chip := singleChipGraph(t)
	var got any
	chip.OnConfig(func(ctx *api.ConfigContext) {
		ctx.GlobalOption("clock").SetType(api.OptionInt).SetDefault(100).
			SetOnApply(func(_ *api.ConfigContext, val any) { got = val })
	})
	cfg := emptyConfig()
	cfg.Global = &config.GlobalConfig{Options: map[string]any{"clock": 200}}
	ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Config: cfg}
	if err := runConfigPhase(ctx); err != nil {
		t.Fatal(err)
	}
	if got != 200 {
		t.Errorf("OnApply saw %v, want configured value 200", got)
	}
}

func TestCollectGlobalMacroDefinesConflict(t *testing.T) {
	cfgCtx := api.NewConfigContextWithPackage("chip", api.NewPackage().SetName("chip"))
	cfgCtx.GlobalOption("a").SetType(api.OptionInt).SetMacroName("CONFIG_SHARED")
	cfgCtx.GlobalOption("b").SetType(api.OptionInt).SetMacroName("CONFIG_SHARED")
	if _, err := collectGlobalMacroDefines(cfgCtx.GetOptions(), map[string]any{"a": 1, "b": 2}); err == nil {
		t.Fatal("expected conflicting global macro error")
	}
	defines, err := collectGlobalMacroDefines(cfgCtx.GetOptions(), map[string]any{"a": 1, "b": 1})
	if err != nil || !reflect.DeepEqual(defines, []string{"CONFIG_SHARED=1"}) {
		t.Fatalf("identical defines should dedupe: %v, %v", defines, err)
	}
}

func TestCollectGlobalMacroDefinesOptInExcluded(t *testing.T) {
	cfgCtx := api.NewConfigContextWithPackage("chip", api.NewPackage().SetName("chip"))
	cfgCtx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")
	cfgCtx.GlobalOption("board").SetType(api.OptionString).SetDefault("evb")
	defines, err := collectGlobalMacroDefines(cfgCtx.GetOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(defines, []string{`CONFIG_BOARD="evb"`}) {
		t.Fatalf("defines = %v, want only CONFIG_BOARD", defines)
	}

	optIn := api.NewConfigContextWithPackage("chip", api.NewPackage().SetName("chip"))
	optIn.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none").
		SetMacroName("CONFIG_TARGET_OS")
	defines, err = collectGlobalMacroDefines(optIn.GetOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(defines, []string{`CONFIG_TARGET_OS="none"`}) {
		t.Fatalf("opt-in defines = %v", defines)
	}
}

func TestGlobalOptionDoesNotFeedPackagedOption(t *testing.T) {
	root := t.TempDir()
	r := resolver.NewResolver(nil, root)
	r.Graph().Order = []string{"alpha", "beta"}
	for _, name := range r.Graph().Order {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		r.Graph().Packages[name] = resolver.NewPackageNode(name,
			buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, api.SourceLocal),
			api.NewPackage().SetName(name))
	}
	r.Graph().Packages["alpha"].Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.GlobalOption("perf").SetType(api.OptionInt).SetDefault(7)
	})
	var got any
	r.Graph().Packages["beta"].Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("perf").SetType(api.OptionBool).SetDefault(true).
			SetOnApply(func(_ *api.ConfigContext, val any) { got = val })
	})
	cfg := emptyConfig()
	cfg.Global = &config.GlobalConfig{Options: map[string]any{"perf": 7}}
	ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Config: cfg}
	if err := runConfigPhase(ctx); err != nil {
		t.Fatal(err)
	}
	if got != true {
		t.Errorf("package option OnApply saw %v, want its own default true", got)
	}
}

func TestGlobalMacroUsesGlobalValueDespiteEntryOverride(t *testing.T) {
	r, chip := singleChipGraph(t)
	chip.OnConfig(func(ctx *api.ConfigContext) {
		ctx.GlobalOption("clock").SetType(api.OptionInt).SetDefault(100)
	})
	cfg := emptyConfig()
	cfg.Global = &config.GlobalConfig{Options: map[string]any{"clock": 200}}
	config.SetEntry(cfg, "chip", &config.EntryConfig{Options: map[string]any{"clock": 99}})
	ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Config: cfg}
	if err := runConfigPhase(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ctx.GlobalMacroDefines, []string{"CONFIG_CLOCK=200"}) {
		t.Fatalf("defines = %v, want the global value 200", ctx.GlobalMacroDefines)
	}
	values, err := PackageConfigValues(ctx, "chip", map[string]any{"clock": 200})
	if err != nil {
		t.Fatal(err)
	}
	if values["clock"] != 99 {
		t.Errorf("package view = %v, want the explicit entry override 99", values["clock"])
	}
}

func contains(slice []string, want string) bool {
	for _, s := range slice {
		if s == want {
			return true
		}
	}
	return false
}
