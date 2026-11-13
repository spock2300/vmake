package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestDeclareTargetsCrossPackageConfig(t *testing.T) {
	owner := localNode("owner")
	ownerAccessor := api.NewConfigAccessor(nil, nil)
	ownerOpts := map[string]*api.Option{
		"feature": ownerAccessor.Option("feature").SetDefault(true),
	}
	owner.Pkg.SetOptions(ownerOpts)

	reader := localNode("reader")
	reader.Pkg.OnBuild(func(ctx *api.BuildContext) {
		kind := api.TargetVoid
		if ctx.Bool("owner/feature") {
			kind = api.TargetObject
		}
		ctx.Target("main").SetKind(kind)
	})

	cfg := emptyConfig()
	ctx := &RuntimeContext{
		Config: cfg,
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"owner":  owner,
			"reader": reader,
		}},
		AllOptions: map[string]map[string]*api.Option{"owner": ownerOpts},
	}
	srcDir := t.TempDir()
	dirs := &api.PkgDirs{SourceDir: srcDir, BuildDir: filepath.Join(srcDir, "build", "key")}

	buildCtx, err := DeclareTargets(ctx, "reader", dirs, testToolchain(), map[string]any{"mode": "debug"})
	if err != nil {
		t.Fatal(err)
	}
	if kind := buildCtx.GetTargets()["main"].Kind(); kind != api.TargetObject {
		t.Fatalf("feature on target kind = %s, want object", kind)
	}

	cfg.Entries["owner"] = &config.EntryConfig{Options: map[string]any{"feature": false}}
	buildCtx, err = DeclareTargets(ctx, "reader", dirs, testToolchain(), map[string]any{"mode": "debug"})
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
		AllOptions: map[string]map[string]*api.Option{"owner": ownerOpts},
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
		AllOptions:    map[string]map[string]*api.Option{},
		GlobalOptions: map[string]*api.Option{"chip": globalOpt},
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
		AllOptions:    map[string]map[string]*api.Option{},
		GlobalOptions: map[string]*api.Option{"chip": globalOpt},
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
		AllOptions:    map[string]map[string]*api.Option{},
		GlobalOptions: map[string]*api.Option{},
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

	root := makeNode("app", "lib")
	root.Pkg.SetRoot(true)
	root.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.GlobalOption("usb").SetDefault(true).SetMacroName("CONFIG_FIRMWARE_USB").SetExportDefine(false)
		ctx.GlobalOption("abi_cap").SetDefault(64)
	})
	lib := makeNode("lib")
	lib.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("local").SetDefault(true)
	})

	rctx.DepGraph.Packages["app"] = root
	rctx.DepGraph.Packages["lib"] = lib
	rctx.DepGraph.Order = []string{"lib", "app"}
	if err := runConfigPhase(rctx); err != nil {
		t.Fatal(err)
	}
	if len(rctx.GlobalMacroDefines) != 1 || rctx.GlobalMacroDefines[0] != "CONFIG_ABI_CAP=64" {
		t.Fatalf("GlobalMacroDefines = %v, want [CONFIG_ABI_CAP=64]", rctx.GlobalMacroDefines)
	}
}

func TestOnConfigureDepsDerivationPrecedenceAndConflicts(t *testing.T) {
	s := sessionFixture(t)
	rctx := s.ctx
	makeNode := func(name string, kind api.SourceOrigin, deps ...string) *resolver.PackageNode {
		dir := t.TempDir()
		return &resolver.PackageNode{
			ID:     name,
			Source: buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, kind),
			Pkg:    api.NewPackage().SetName(name),
			Deps:   deps,
		}
	}

	leaf := makeNode("sdk/leaf", api.SourceRemote)
	leaf.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("bt").SetDefault(false)
		ctx.Option("mode").SetDefault("device").SetValues("device", "host")
	})

	mid := makeNode("mid", api.SourceLocal, "sdk/leaf")
	mid.Pkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		ctx.SetDefault("sdk/leaf", "bt", ctx.Bool("ble"))
		ctx.SetDefault("sdk/leaf", "mode", "host")
	})

	app := makeNode("app", api.SourceLocal, "mid", "sdk/leaf")
	app.Pkg.SetRoot(true)
	app.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.GlobalOption("ble").SetDefault(true).SetExportDefine(false)
	})

	rctx.DepGraph.Packages = map[string]*resolver.PackageNode{
		"sdk/leaf": leaf,
		"mid":      mid,
		"app":      app,
	}
	rctx.DepGraph.Order = []string{"sdk/leaf", "mid", "app"}
	if err := runConfigPhase(rctx); err != nil {
		t.Fatal(err)
	}
	needed := map[string]bool{"sdk/leaf": true, "mid": true, "app": true}
	if err := resolveDerivedOptions(rctx, needed); err != nil {
		t.Fatal(err)
	}

	vals, err := PackageConfigValues(rctx, "sdk/leaf", projectGlobalValues(rctx))
	if err != nil {
		t.Fatal(err)
	}
	if vals["bt"] != true || vals["mode"] != "host" {
		t.Fatalf("derived leaf values = %v, want bt=true mode=host", vals)
	}

	allOpts := collectAllPkgOptions(rctx, needed)
	if allOpts["sdk/leaf"]["bt"] != true || allOpts["sdk/leaf"]["mode"] != "host" {
		t.Fatalf("leaf BuildKey options = %v, want derived bt=true mode=host", allOpts["sdk/leaf"])
	}
	if allOpts["mid"]["ble"] != true || allOpts["app"]["ble"] != true {
		t.Fatalf("SetExportDefine(false) global missing from local workspace BuildKey options: mid=%v app=%v", allOpts["mid"], allOpts["app"])
	}
	if _, leaked := allOpts["sdk/leaf"]["ble"]; leaked {
		t.Fatalf("SetExportDefine(false) global leaked into remote SDK leaf BuildKey: %v", allOpts["sdk/leaf"])
	}

	rctx.Config.Entries["sdk/leaf"] = &config.EntryConfig{Options: map[string]any{"mode": "device"}}
	vals, err = PackageConfigValues(rctx, "sdk/leaf", projectGlobalValues(rctx))
	if err != nil {
		t.Fatal(err)
	}
	if vals["mode"] != "device" {
		t.Fatalf("explicit config override should beat OnConfigureDeps, got mode=%v", vals["mode"])
	}

	app.Pkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		ctx.SetDefault("sdk/leaf", "bt", false)
	})
	if err := resolveDerivedOptions(rctx, needed); err == nil || !strings.Contains(err.Error(), "conflicting OnConfigureDeps default") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestConvergeFilterDepsSeesParentDerivedOptionsAndRootOverride(t *testing.T) {
	s := sessionFixture(t)
	rctx := s.ctx
	makeNode := func(name string, kind api.SourceOrigin, deps ...string) *resolver.PackageNode {
		dir := t.TempDir()
		path := filepath.Join(dir, "build.go")
		if err := os.WriteFile(path, []byte("package main\nimport \"github.com/spock2300/vmake/pkg/api\"\nfunc Main(p *api.Package) {}\n"), 0644); err != nil {
			t.Fatal(err)
		}
		src := buildscript.NewSource(name, path, dir, kind)
		rctx.Resolver.Graph().Packages[name] = &resolver.PackageNode{
			ID:     name,
			Source: src,
			Pkg:    api.NewPackage().SetName(name),
			Deps:   deps,
		}
		return rctx.Resolver.Graph().Packages[name]
	}

	rctx.DepGraph.Packages = make(map[string]*resolver.PackageNode)
	radioDrv := makeNode("sdk/radio", api.SourceRemote)
	board := makeNode("board", api.SourceLocal, "sdk/radio")
	board.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("radio").SetDefault(false)
	})
	board.Pkg.OnRequire(func(ctx *api.RequireContext) {
		if ctx.When("radio", true) {
			ctx.AddRequires("sdk/radio")
		}
	})

	fw := makeNode("firmware", api.SourceLocal, "board")
	fw.Pkg.SetRoot(true)
	fw.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("enable_radio").SetDefault(false)
	})
	fw.Pkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		ctx.SetDefault("board", "radio", ctx.Bool("enable_radio"))
	})

	altApp := makeNode("target-app", api.SourceLocal, "board")
	altApp.Pkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		ctx.SetDefault("board", "radio", false)
	})

	localSources := []buildscript.Source{*radioDrv.Source, *board.Source, *fw.Source, *altApp.Source}
	r := resolver.NewResolver(nil, filepath.Join(t.TempDir(), ".vmake_deps"))
	if err := r.ResolveAll(localSources); err != nil {
		t.Fatal(err)
	}
	r.Graph().Packages["sdk/radio"] = radioDrv
	r.Graph().Packages["board"] = board
	r.Graph().Packages["firmware"] = fw
	r.Graph().Packages["target-app"] = altApp
	if err := r.UpdateOrder(); err != nil {
		t.Fatal(err)
	}
	rctx.Resolver = r
	rctx.DepGraph = r.Graph()
	rctx.Config.Entries["firmware"] = &config.EntryConfig{Options: map[string]any{"enable_radio": true}}

	if err := runConfigPhase(rctx); err != nil {
		t.Fatal(err)
	}
	needed, err := resolveNeeded(rctx, false, false, false)
	if err != nil {
		t.Fatalf("resolveNeeded: %v", err)
	}
	if !needed["firmware"] || !needed["board"] || !needed["sdk/radio"] || needed["target-app"] {
		t.Fatalf("needed = %v, want firmware+board+sdk/radio only", needed)
	}

	rctx.Config = emptyConfig()
	rctx.Config.Root = "target-app"
	board.Deps = []string{"sdk/radio"}
	if err := r.UpdateOrder(); err != nil {
		t.Fatal(err)
	}
	if err := runConfigPhase(rctx); err != nil {
		t.Fatal(err)
	}
	needed2, err := resolveNeeded(rctx, false, false, false)
	if err != nil {
		t.Fatalf("resolveNeeded with root override: %v", err)
	}
	if !needed2["target-app"] || !needed2["board"] || needed2["firmware"] || needed2["sdk/radio"] {
		t.Fatalf("needed2 = %v, want target-app+board only", needed2)
	}

	rctx.Config.Entries["firmware"] = &config.EntryConfig{Options: map[string]any{"enable_radio": true}}
	rctx.Config.Entries["board"] = &config.EntryConfig{Options: map[string]any{"typo": true}}
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
	makeNode := func(name string, kind api.SourceOrigin, deps ...string) *resolver.PackageNode {
		dir := t.TempDir()
		return &resolver.PackageNode{
			ID:     name,
			Source: buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, kind),
			Pkg:    api.NewPackage().SetName(name),
			Deps:   deps,
		}
	}
	board := makeNode("leaf", api.SourceLocal)
	board.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("uart").SetDefault(false)
		ctx.GlobalOption("chip").SetType(api.OptionChoice).SetDefault("c1").SetValues("c1")
	})

	app := makeNode("app", api.SourceLocal, "leaf")
	app.Pkg.SetRoot(true)
	var hasUart, hasRadio, hasGlobalChip bool
	app.Pkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		hasUart = ctx.HasOption("leaf", "uart")
		hasRadio = ctx.HasOption("leaf", "radio")
		hasGlobalChip = ctx.HasOption("leaf", "chip")
		if hasUart {
			ctx.SetDefault("leaf", "uart", true)
		}
		if hasRadio {
			ctx.SetDefault("leaf", "radio", true)
		}
	})

	rctx.DepGraph.Packages = map[string]*resolver.PackageNode{
		"leaf": board,
		"app":  app,
	}
	rctx.DepGraph.Order = []string{"leaf", "app"}

	if err := runConfigPhase(rctx); err != nil {
		t.Fatal(err)
	}
	needed := map[string]bool{"leaf": true, "app": true}
	if err := resolveDerivedOptions(rctx, needed); err != nil {
		t.Fatalf("resolveDerivedOptions: %v", err)
	}
	if !hasUart || hasRadio || hasGlobalChip {
		t.Fatalf("HasOption results: uart=%v radio=%v chip=%v, want true/false/false", hasUart, hasRadio, hasGlobalChip)
	}
	vals, err := PackageConfigValues(rctx, "leaf", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vals["uart"] != true {
		t.Fatalf("leaf/uart = %v, want true", vals["uart"])
	}
}
