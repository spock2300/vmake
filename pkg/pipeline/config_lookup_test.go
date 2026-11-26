package pipeline

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestDeclareTargetsCrossPackageConfig(t *testing.T) {
	owner := localNode("//owner")
	ownerAccessor := api.NewConfigAccessor(nil, nil)
	ownerOpts := map[string]*api.Option{
		"feature": ownerAccessor.Option("feature").SetDefault(true),
	}
	owner.Pkg.SetOptions(ownerOpts)

	reader := localNode("//reader")
	reader.Pkg.OnBuild(func(ctx *api.BuildContext) {
		kind := api.TargetVoid
		if ctx.Bool("//owner:feature") {
			kind = api.TargetObject
		}
		ctx.Target("main").SetKind(kind)
	})

	cfg := emptyConfig()
	ctx := &RuntimeContext{
		Config: cfg,
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"//owner":  owner,
			"//reader": reader,
		}},
		allOptions: map[string]map[string]*api.Option{"//owner": ownerOpts},
	}
	srcDir := t.TempDir()
	dirs := &api.PkgDirs{SourceDir: srcDir, BuildDir: filepath.Join(srcDir, "build", "key")}

	buildCtx, err := DeclareTargets(ctx, "//reader", dirs, testToolchain(), map[string]any{"mode": "debug"})
	if err != nil {
		t.Fatal(err)
	}
	if kind := buildCtx.GetTargets()["main"].Kind(); kind != api.TargetObject {
		t.Fatalf("feature on target kind = %s, want object", kind)
	}

	cfg.Entries["//owner"] = &config.EntryConfig{Options: map[string]any{"feature": false}}
	buildCtx, err = DeclareTargets(ctx, "//reader", dirs, testToolchain(), map[string]any{"mode": "debug"})
	if err != nil {
		t.Fatal(err)
	}
	if kind := buildCtx.GetTargets()["main"].Kind(); kind != api.TargetVoid {
		t.Fatalf("feature off target kind = %s, want void", kind)
	}
}

func TestConfigLookupUsesBoundValuesAndReportsMissing(t *testing.T) {
	owner := localNode("owner")
	ownerAccessor := api.NewConfigAccessor(nil, nil)
	ownerOpts := map[string]*api.Option{
		"feature": ownerAccessor.Option("feature").SetDefault(true),
	}
	owner.Pkg.SetOptions(ownerOpts)

	ctx := &RuntimeContext{
		Config: emptyConfig(),
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"owner": owner,
		}},
		allOptions: map[string]map[string]*api.Option{"owner": ownerOpts},
	}
	lookup := newConfigLookup(ctx, "", nil, func(pkg string) (map[string]any, bool) {
		if pkg != "owner" {
			return nil, false
		}
		return map[string]any{"feature": false}, true
	})
	value, opt, err := lookup("owner", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if value != false || opt == nil {
		t.Fatalf("bound lookup = %v/%v, want false with option", value, opt)
	}
	if _, _, err := lookup("missing", "feature"); err == nil {
		t.Error("unknown package should fail")
	}
	if _, _, err := lookup("owner", "missing"); err == nil {
		t.Error("unknown option should fail")
	}
}

func TestConfigLookupGlobalOption(t *testing.T) {
	reader := localNode("reader")
	globalAccessor := api.NewConfigAccessor(nil, nil)
	globalOpt := globalAccessor.Option("chip").SetDefault("py32f539").SetGroup(api.GroupGlobal)
	ctx := &RuntimeContext{
		Config: emptyConfig(),
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"reader": reader,
		}},
		allOptions:    map[string]map[string]*api.Option{},
		globalOptions: map[string]*api.Option{"chip": globalOpt},
	}
	lookup := newConfigLookup(ctx, "reader", map[string]any{"chip": "py32f537"}, nil)
	value, opt, err := lookup("reader", "chip")
	if err != nil {
		t.Fatal(err)
	}
	if value != "py32f537" || opt != globalOpt {
		t.Fatalf("global lookup = %v/%v, want py32f537 with global option", value, opt)
	}
}

func TestConfigLookupGlobalOptionUsesPackageOverride(t *testing.T) {
	reader := localNode("reader")
	globalAccessor := api.NewConfigAccessor(nil, nil)
	globalOpt := globalAccessor.Option("chip").SetDefault("py32f539").SetGroup(api.GroupGlobal)
	cfg := emptyConfig()
	cfg.Entries["reader"] = &config.EntryConfig{Options: map[string]any{"chip": "py32f537"}}
	ctx := &RuntimeContext{
		Config: cfg,
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"reader": reader,
		}},
		allOptions:    map[string]map[string]*api.Option{},
		globalOptions: map[string]*api.Option{"chip": globalOpt},
	}
	lookup := newConfigLookup(ctx, "reader", map[string]any{"chip": "py32f539"}, nil)
	value, opt, err := lookup("reader", "chip")
	if err != nil {
		t.Fatal(err)
	}
	if value != "py32f537" || opt != globalOpt {
		t.Fatalf("global lookup = %v/%v, want package override py32f537", value, opt)
	}
}

func TestConfigLookupImplicitPlatformOption(t *testing.T) {
	reader := localNode("reader")
	ctx := &RuntimeContext{
		Config: emptyConfig(),
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"reader": reader,
		}},
		allOptions:    map[string]map[string]*api.Option{},
		globalOptions: map[string]*api.Option{},
	}
	lookup := newConfigLookup(ctx, "reader", map[string]any{}, nil)
	value, opt, err := lookup("reader", api.TargetOSOptionName)
	if err != nil {
		t.Fatal(err)
	}
	if opt == nil || opt.Type() != api.OptionString {
		t.Fatalf("opt = %v, want a string option", opt)
	}
	if _, ok := value.(string); !ok {
		t.Fatalf("value = %#v, want a string", value)
	}
}

func TestGlobalOptionSetExportDefineFalseSuppressesGlobalMacro(t *testing.T) {
	s := sessionFixture(t)
	rctx := s.ctx
	makeNode := func(name string, deps ...string) *resolver.PackageNode {
		dir := t.TempDir()
		return &resolver.PackageNode{
			ID:     name,
			Source: buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, api.SourceLocal),
			Pkg:    api.NewPackage().SetName(name),
			Deps:   deps,
		}
	}

	root := makeNode("//app", "//lib")
	root.Container = "//"
	root.Kind = resolver.SourceKindLocal
	root.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.GlobalOption("usb").SetDefault(true).SetMacroName("CONFIG_FIRMWARE_USB").SetExportDefine(false)
		ctx.GlobalOption("abi_cap").SetDefault(64)
	})
	lib := makeNode("//lib")
	lib.Container = "//"
	lib.Kind = resolver.SourceKindLocal
	lib.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("local").SetDefault(true)
	})

	rctx.DepGraph.Roots = []string{"//app"}
	rctx.DepGraph.Packages = map[string]*resolver.PackageNode{"//app": root, "//lib": lib}
	rctx.DepGraph.Order = []string{"//lib", "//app"}
	if err := runConfigPhase(rctx); err != nil {
		t.Fatal(err)
	}
	if len(rctx.globalMacroDefines) != 1 || rctx.globalMacroDefines[0] != "CONFIG_ABI_CAP=64" {
		t.Fatalf("GlobalMacroDefines = %v, want [CONFIG_ABI_CAP=64]", rctx.globalMacroDefines)
	}
}

func TestOnConfigureDepsDerivationPrecedenceAndConflicts(t *testing.T) {
	s := sessionFixture(t)
	rctx := s.ctx
	makeNode := func(name, container string, kind resolver.SourceKind, deps ...string) *resolver.PackageNode {
		dir := t.TempDir()
		origin := api.SourceRemote
		if kind == resolver.SourceKindLocal {
			origin = api.SourceLocal
		}
		return &resolver.PackageNode{
			ID:        name,
			Container: container,
			Kind:      kind,
			Source:    buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, origin),
			Pkg:       api.NewPackage().SetName(name),
			Deps:      deps,
		}
	}

	leaf := makeNode("sdk/mod/leaf", "sdk/mod", resolver.SourceKindNative)
	leaf.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("bt").SetDefault(false)
		ctx.Option("mode").SetDefault("device").SetValues("device", "host")
	})

	mid := makeNode("//mid", "//", resolver.SourceKindLocal, "sdk/mod/leaf")
	mid.Pkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		ctx.SetDefault("sdk/mod/leaf", "bt", ctx.Bool("ble"))
		ctx.SetDefault("sdk/mod/leaf", "mode", "host")
	})

	app := makeNode("//app", "//", resolver.SourceKindLocal, "//mid", "sdk/mod/leaf")
	app.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.GlobalOption("ble").SetDefault(true).SetExportDefine(false)
	})

	rctx.DepGraph.Roots = []string{"//app"}
	rctx.DepGraph.Packages = map[string]*resolver.PackageNode{
		"sdk/mod/leaf": leaf,
		"//mid":        mid,
		"//app":        app,
	}
	rctx.DepGraph.Order = []string{"sdk/mod/leaf", "//mid", "//app"}
	if err := runConfigPhase(rctx); err != nil {
		t.Fatal(err)
	}
	needed := map[string]bool{"sdk/mod/leaf": true, "//mid": true, "//app": true}
	if err := resolveDerivedOptions(rctx, needed); err != nil {
		t.Fatal(err)
	}

	vals, err := PackageConfigValues(rctx, "sdk/mod/leaf", ProjectGlobalValues(rctx))
	if err != nil {
		t.Fatal(err)
	}
	if vals["bt"] != true || vals["mode"] != "host" {
		t.Fatalf("derived leaf values = %v, want bt=true mode=host", vals)
	}

	allOpts := collectAllPkgOptions(rctx, needed)
	if allOpts["sdk/mod/leaf"]["bt"] != true || allOpts["sdk/mod/leaf"]["mode"] != "host" {
		t.Fatalf("leaf BuildKey options = %v, want derived bt=true mode=host", allOpts["sdk/mod/leaf"])
	}
	if allOpts["//mid"]["ble"] != true || allOpts["//app"]["ble"] != true {
		t.Fatalf("SetExportDefine(false) global missing from local workspace BuildKey options: mid=%v app=%v", allOpts["//mid"], allOpts["//app"])
	}
	if _, leaked := allOpts["sdk/mod/leaf"]["ble"]; leaked {
		t.Fatalf("SetExportDefine(false) global leaked into remote SDK leaf BuildKey: %v", allOpts["sdk/mod/leaf"])
	}

	rctx.Config.Entries["sdk/mod/leaf"] = &config.EntryConfig{Options: map[string]any{"mode": "device"}}
	vals, err = PackageConfigValues(rctx, "sdk/mod/leaf", ProjectGlobalValues(rctx))
	if err != nil {
		t.Fatal(err)
	}
	if vals["mode"] != "device" {
		t.Fatalf("explicit config override should beat OnConfigureDeps, got mode=%v", vals["mode"])
	}

	app.Pkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		ctx.SetDefault("sdk/mod/leaf", "bt", false)
	})
	if err := resolveDerivedOptions(rctx, needed); err == nil || !strings.Contains(err.Error(), "conflicting OnConfigureDeps default") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestConvergeFilterDepsSeesParentDerivedOptionsAndRootOverride(t *testing.T) {
	s := sessionFixture(t)
	rctx := s.ctx
	makeNode := func(name, container string, kind resolver.SourceKind, deps ...string) *resolver.PackageNode {
		dir := t.TempDir()
		origin := api.SourceRemote
		if kind == resolver.SourceKindLocal {
			origin = api.SourceLocal
		}
		src := buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, origin)
		return &resolver.PackageNode{
			ID:        name,
			Container: container,
			Kind:      kind,
			Source:    src,
			Pkg:       api.NewPackage().SetName(name),
			Deps:      deps,
		}
	}

	radioDrv := makeNode("sdk/mod/radio", "sdk/mod", resolver.SourceKindNative)
	board := makeNode("//board", "//", resolver.SourceKindLocal, "sdk/mod/radio")
	board.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("radio").SetDefault(false)
	})
	board.Pkg.OnRequire(func(ctx *api.RequireContext) {
		if ctx.When("radio", true) {
			ctx.AddRequires("sdk/mod/radio")
		}
	})

	fw := makeNode("//firmware", "//", resolver.SourceKindLocal, "//board")
	fw.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("enable_radio").SetDefault(false)
	})
	fw.Pkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		ctx.SetDefault("//board", "radio", ctx.Bool("enable_radio"))
	})

	altApp := makeNode("//target-app", "//", resolver.SourceKindLocal, "//board")
	altApp.Pkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		ctx.SetDefault("//board", "radio", false)
	})

	r := resolver.NewResolver(nil, filepath.Join(t.TempDir(), ".vmake_deps"))
	r.Graph().Roots = []string{"//firmware"}
	r.Graph().Packages["sdk/mod/radio"] = radioDrv
	r.Graph().Packages["//board"] = board
	r.Graph().Packages["//firmware"] = fw
	r.Graph().Packages["//target-app"] = altApp
	if err := r.UpdateOrder(); err != nil {
		t.Fatal(err)
	}
	rctx.Resolver = r
	rctx.DepGraph = r.Graph()
	rctx.Config.Entries["//firmware"] = &config.EntryConfig{Options: map[string]any{"enable_radio": true}}

	if err := runConfigPhase(rctx); err != nil {
		t.Fatal(err)
	}
	needed, err := resolveNeeded(rctx, false, false)
	if err != nil {
		t.Fatalf("resolveNeeded: %v", err)
	}
	if !needed["//firmware"] || !needed["//board"] || !needed["sdk/mod/radio"] || needed["//target-app"] {
		t.Fatalf("needed = %v, want //firmware+//board+sdk/mod/radio only", needed)
	}

	rctx.Config = emptyConfig()
	rctx.Config.Root = "//target-app"
	board.Deps = []string{"sdk/mod/radio"}
	if err := r.UpdateOrder(); err != nil {
		t.Fatal(err)
	}
	if err := runConfigPhase(rctx); err != nil {
		t.Fatal(err)
	}
	needed2, err := resolveNeeded(rctx, false, false)
	if err != nil {
		t.Fatalf("resolveNeeded with root override: %v", err)
	}
	if !needed2["//target-app"] || !needed2["//board"] || needed2["//firmware"] || needed2["sdk/mod/radio"] {
		t.Fatalf("needed2 = %v, want //target-app+//board only", needed2)
	}

	rctx.Config.Entries["//firmware"] = &config.EntryConfig{Options: map[string]any{"enable_radio": true}}
	rctx.Config.Entries["//board"] = &config.EntryConfig{Options: map[string]any{"typo": true}}
	issues, err := ValidateConfigStrict(rctx)
	if err != nil {
		t.Fatalf("ValidateConfigStrict error: %v", err)
	}
	if len(issues) < 2 {
		t.Fatalf("expected >=2 strict issues, got %v", issues)
	}
}

func TestOnConfigureDepsHasOptionHeterogeneousBoard(t *testing.T) {
	s := sessionFixture(t)
	rctx := s.ctx
	makeNode := func(name string, deps ...string) *resolver.PackageNode {
		dir := t.TempDir()
		return &resolver.PackageNode{
			ID:        name,
			Container: "//",
			Kind:      resolver.SourceKindLocal,
			Source:    buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, api.SourceLocal),
			Pkg:       api.NewPackage().SetName(name),
			Deps:      deps,
		}
	}
	board := makeNode("//leaf")
	board.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("uart").SetDefault(false)
		ctx.GlobalOption("chip").SetType(api.OptionChoice).SetDefault("c1").SetValues("c1")
	})

	app := makeNode("//app", "//leaf")
	var hasUart, hasRadio, hasGlobalChip bool
	app.Pkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		hasUart = ctx.HasOption("//leaf", "uart")
		hasRadio = ctx.HasOption("//leaf", "radio")
		hasGlobalChip = ctx.HasOption("//leaf", "chip")
		if hasUart {
			ctx.SetDefault("//leaf", "uart", true)
		}
		if hasRadio {
			ctx.SetDefault("//leaf", "radio", true)
		}
	})

	rctx.DepGraph.Roots = []string{"//app"}
	rctx.DepGraph.Packages = map[string]*resolver.PackageNode{
		"//leaf": board,
		"//app":  app,
	}
	rctx.DepGraph.Order = []string{"//leaf", "//app"}

	if err := runConfigPhase(rctx); err != nil {
		t.Fatal(err)
	}
	needed := map[string]bool{"//leaf": true, "//app": true}
	if err := resolveDerivedOptions(rctx, needed); err != nil {
		t.Fatalf("resolveDerivedOptions: %v", err)
	}
	if !hasUart || hasRadio || hasGlobalChip {
		t.Fatalf("HasOption results: uart=%v radio=%v chip=%v, want true/false/false", hasUart, hasRadio, hasGlobalChip)
	}
	vals, err := PackageConfigValues(rctx, "//leaf", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vals["uart"] != true {
		t.Fatalf("//leaf:uart = %v, want true", vals["uart"])
	}
}

func TestConfigLookupHidesInternalOptionsInErrorHintAcrossContainers(t *testing.T) {
	reader := localNode("//app")
	ext := &resolver.PackageNode{
		ID:        "org/sdk/core",
		Container: "org/sdk",
		RelPath:   "core",
		Exported:  true,
		Source:    &buildscript.Source{Name: "org/sdk/core", Origin: api.SourceRemote},
		Pkg:       api.NewPackage(),
	}
	extAccessor := api.NewConfigAccessor(nil, nil)
	extOpts := map[string]*api.Option{
		"public_opt": extAccessor.Option("public_opt").SetDefault(true),
		"secret_opt": extAccessor.Option("secret_opt").SetDefault(false).SetInternal(true),
	}
	ext.Pkg.SetOptions(extOpts)

	ctx := &RuntimeContext{
		Config: emptyConfig(),
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"//app":        reader,
			"org/sdk/core": ext,
		}},
		allOptions: map[string]map[string]*api.Option{
			"org/sdk/core": extOpts,
		},
		globalOptions: map[string]*api.Option{},
	}

	lookup := newConfigLookup(ctx, "//app", nil, nil)
	_, _, err := lookup("org/sdk/core", "nonexistent")
	if err == nil {
		t.Fatal("expected unknown option error")
	}
	if strings.Contains(err.Error(), "secret_opt") {
		t.Fatalf("cross-container unknown-option hint leaked internal option: %v", err)
	}
	if !strings.Contains(err.Error(), "public_opt") {
		t.Fatalf("expected public_opt in available hint: %v", err)
	}
}
