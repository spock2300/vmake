package pipeline

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func testBoolOption() *api.Option {
	return (&api.Option{}).SetType(api.OptionBool)
}

func TestResolveSetPackageOptionCanonicalIDAndMutateConfig(t *testing.T) {
	name := "vendor/sdk/kernel"
	ctx := &RuntimeContext{
		Config: &config.ConfigFile{
			Global:  &config.GlobalConfig{Options: map[string]any{}},
			Entries: map[string]*config.EntryConfig{"stale/pkg": {Options: map[string]any{"old": true}}},
		},
		allOptions: map[string]map[string]*api.Option{
			name: {"debug": testBoolOption()},
		},
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			name: {ID: name},
		}},
	}
	resolved, opt, err := ResolveSetPackageOption(ctx, name, "debug")
	if err != nil || resolved != name || opt == nil || opt.Type() != api.OptionBool {
		t.Fatalf("resolved = %q, opt = %+v, err = %v", resolved, opt, err)
	}

	changes, err := MutateConfig(ctx, ConfigMutation{
		Unsets: []UnsetItem{{Pkg: "stale/pkg", Opt: "old"}},
		Sets:   []SetItem{{Pkg: name, Opt: "debug", Raw: "true"}},
	})
	if err != nil {
		t.Fatalf("MutateConfig: %v", err)
	}
	if len(changes) != 2 || changes[1].Pkg != name || changes[1].Value != true || changes[1].Key() != name+":debug" {
		t.Fatalf("changes = %+v", changes)
	}
	if ctx.Config.HasExplicitOption("stale/pkg", "old") {
		t.Fatal("stale explicit key should be unset")
	}
	if got := config.GetEntry(ctx.Config, name).Options["debug"]; got != true {
		t.Fatalf("%s:debug = %v, want true", name, got)
	}

	if _, _, err := ResolveSetPackageOption(ctx, "kernel", "debug"); err == nil || !strings.Contains(err.Error(), `package "kernel" not found`) {
		t.Fatalf("unqualified short name should be rejected, got %v", err)
	}
}

func TestInternalOptionVisibilityAcrossContainerSeam(t *testing.T) {
	extContainer := "vendor/sdk"
	extFacade := "vendor/sdk/graphics"
	extLeaf := "vendor/sdk/lvgl"
	localApp := "//app"
	localLeaf := "//local-leaf"

	facadePkg := api.NewPackage().SetName(extFacade)
	facadePkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("enabled").SetDefault(true)
	})
	facadePkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		if !ctx.HasOption("//lvgl", "mem_size") {
			panic("same-container HasOption must see internal option")
		}
		ctx.SetDefault("//lvgl", "mem_size", 8192)
	})
	leafPkg := api.NewPackage().SetName(extLeaf)
	leafPkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("mem_size").SetDefault(4096).SetInternal(true)
		ctx.Option("public_flag").SetDefault(true)
	})
	appPkg := api.NewPackage().SetName(localApp)
	localLeafPkg := api.NewPackage().SetName(localLeaf)
	localLeafPkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("secret").SetDefault(4096).SetInternal(true)
	})

	tmp := t.TempDir()
	r := resolver.NewResolver(nil, tmp)
	r.Graph().Containers[api.LocalContainerID] = &resolver.ContainerNode{ID: api.LocalContainerID, Kind: resolver.SourceKindLocal, Dir: tmp}
	r.Graph().Containers[extContainer] = &resolver.ContainerNode{ID: extContainer, Kind: resolver.SourceKindNative, Dir: tmp}
	r.Graph().Packages[localApp] = &resolver.PackageNode{ID: localApp, Container: api.LocalContainerID, Kind: resolver.SourceKindLocal, Source: buildscript.NewSource(localApp, tmp+"/build.go", tmp, api.SourceLocal), Pkg: appPkg, Deps: []string{extFacade, localLeaf}}
	r.Graph().Packages[localLeaf] = &resolver.PackageNode{ID: localLeaf, Container: api.LocalContainerID, Kind: resolver.SourceKindLocal, Source: buildscript.NewSource(localLeaf, tmp+"/build.go", tmp, api.SourceLocal), Pkg: localLeafPkg}
	r.Graph().Packages[extFacade] = &resolver.PackageNode{ID: extFacade, Container: extContainer, Kind: resolver.SourceKindNative, Source: buildscript.NewSource(extFacade, tmp+"/build.go", tmp, api.SourceRemote), Pkg: facadePkg, Deps: []string{extLeaf}}
	r.Graph().Packages[extLeaf] = &resolver.PackageNode{ID: extLeaf, Container: extContainer, Kind: resolver.SourceKindNative, Source: buildscript.NewSource(extLeaf, tmp+"/build.go", tmp, api.SourceRemote), Pkg: leafPkg}
	r.Graph().Roots = []string{localApp}
	r.Graph().Order = []string{extLeaf, extFacade, localLeaf, localApp}

	internalInt := (&api.Option{}).SetDefault(4096).SetInternal(true)
	publicBool := (&api.Option{}).SetDefault(true)
	ctx := &RuntimeContext{
		Resolver: r,
		DepGraph: r.Graph(),
		Paths:    &Paths{ProjectDir: tmp, DepsDir: tmp},
		Config: &config.ConfigFile{
			Global:  &config.GlobalConfig{Options: map[string]any{}},
			Entries: map[string]*config.EntryConfig{},
		},
		allOptions: map[string]map[string]*api.Option{
			extFacade: {"enabled": publicBool},
			extLeaf:   {"mem_size": internalInt, "public_flag": publicBool},
			localLeaf: {"secret": internalInt},
		},
	}

	if _, _, err := ResolveSetPackageOption(ctx, extLeaf, "mem_size"); err == nil || !strings.Contains(err.Error(), "internal") {
		t.Fatalf("external internal option should be rejected in ResolveSetPackageOption, got %v", err)
	}
	if _, _, err := ResolveSetPackageOption(ctx, extLeaf, "public_flag"); err != nil {
		t.Fatalf("external public option should be allowed, got %v", err)
	}
	if _, _, err := ResolveSetPackageOption(ctx, localLeaf, "secret"); err != nil {
		t.Fatalf("local internal option should be allowed inside local container, got %v", err)
	}
	wb := NewConfigWorkbench(ctx)
	if wb == nil {
		t.Fatal("NewConfigWorkbench returned nil")
	}
	if _, exists := wb.Options[extLeaf]["mem_size"]; exists {
		t.Fatal("external internal option must be hidden from TUI workbench")
	}
	if _, exists := wb.Options[extLeaf]["public_flag"]; !exists {
		t.Fatal("external public option must remain visible in TUI workbench")
	}
	ctx.Config.SetOption(extLeaf, "mem_size", 16384)
	if err := Configure(ctx); err == nil || !strings.Contains(err.Error(), "internal") {
		t.Fatalf("Configure should reject external internal option before FilterDeps, got %v", err)
	}
	if err := ValidateConfigValues(ctx, nil); err == nil || !strings.Contains(err.Error(), "internal") {
		t.Fatalf("ValidateConfigValues should reject external internal option, got %v", err)
	}
	ctx.Config.UnsetOption(extLeaf, "mem_size")
	needed := NeededSet{localApp: true, localLeaf: true, extFacade: true, extLeaf: true}
	if err := resolveDerivedOptions(ctx, needed); err != nil {
		t.Fatalf("same-container OnConfigureDeps failed: %v", err)
	}
	if got := ctx.derivedOptions[extLeaf]["mem_size"]; got != 8192 {
		t.Fatalf("derived mem_size = %v, want 8192", got)
	}
	appPkg.OnConfigureDeps(func(dctx *api.DepConfigContext) {
		if dctx.HasOption(extLeaf, "mem_size") {
			panic("cross-container HasOption must not see internal option")
		}
		dctx.SetDefault(extLeaf, "mem_size", 1024)
	})
	if err := resolveDerivedOptions(ctx, needed); err == nil || !strings.Contains(err.Error(), "internal") {
		t.Fatalf("cross-container OnConfigureDeps should reject external internal option, got %v", err)
	}
}

func TestContainerExportsEnforcedAcrossConfigWorkbenchAndBuildGraph(t *testing.T) {
	tmp := t.TempDir()
	extContainer := "vendor/sdk"
	extFacade := "vendor/sdk/graphics"
	extHidden := "vendor/sdk/system/freertos/lvgl"
	localApp := "//app"

	manifest := &buildscript.Manifest{
		Exports: map[string]string{
			"graphics": "//sdk/graphics",
		},
	}
	facadePkg := api.NewPackage().SetName(extFacade)
	hiddenPkg := api.NewPackage().SetName(extHidden)
	appPkg := api.NewPackage().SetName(localApp)

	r := resolver.NewResolver(nil, tmp)
	r.Graph().Containers[api.LocalContainerID] = &resolver.ContainerNode{ID: api.LocalContainerID, Kind: resolver.SourceKindLocal, Dir: tmp}
	r.Graph().Containers[extContainer] = &resolver.ContainerNode{ID: extContainer, Kind: resolver.SourceKindNative, Dir: tmp, Manifest: manifest}
	r.Graph().Packages[localApp] = &resolver.PackageNode{ID: localApp, Container: api.LocalContainerID, Kind: resolver.SourceKindLocal, Exported: true, Source: buildscript.NewSource(localApp, tmp+"/build.go", tmp, api.SourceLocal), Pkg: appPkg, Deps: []string{extFacade}}
	r.Graph().Packages[extFacade] = &resolver.PackageNode{ID: extFacade, Container: extContainer, RelPath: "sdk/graphics", Kind: resolver.SourceKindNative, Exported: true, Source: buildscript.NewSource(extFacade, tmp+"/build.go", tmp, api.SourceRemote), Pkg: facadePkg, Deps: []string{extHidden}}
	r.Graph().Packages[extHidden] = &resolver.PackageNode{ID: extHidden, Container: extContainer, RelPath: "system/freertos/lvgl", Kind: resolver.SourceKindNative, Exported: false, Source: buildscript.NewSource(extHidden, tmp+"/build.go", tmp, api.SourceRemote), Pkg: hiddenPkg}
	r.Graph().Roots = []string{localApp}
	r.Graph().Order = []string{extHidden, extFacade, localApp}

	ctx := &RuntimeContext{
		Resolver: r,
		DepGraph: r.Graph(),
		Paths:    &Paths{ProjectDir: tmp, DepsDir: tmp},
		Config: &config.ConfigFile{
			Global:  &config.GlobalConfig{Options: map[string]any{}},
			Entries: map[string]*config.EntryConfig{},
		},
		allOptions: map[string]map[string]*api.Option{
			extFacade: {"fps": testBoolOption(), "secret_knob": testBoolOption().SetInternal(true)},
			extHidden: {"raw_knob": testBoolOption()},
		},
		allKConfigs: map[string][]*api.KConfigEntry{
			extHidden: {hiddenPkg.AddKConfig("hidden_kcfg")},
		},
		derivedOptions: map[string]map[string]any{
			extFacade: {"fps": true, "secret_knob": true},
			extHidden: {"raw_knob": true},
		},
	}

	if _, _, err := ResolveSetPackageOption(ctx, extHidden, "raw_knob"); err == nil || !strings.Contains(err.Error(), "not exported") {
		t.Fatalf("ResolveSetPackageOption(non-exported) = %v, want not exported error", err)
	}
	if _, _, err := ResolveSetPackageOption(ctx, extFacade, "fps"); err != nil {
		t.Fatalf("ResolveSetPackageOption(exported) = %v", err)
	}
	wb := NewConfigWorkbench(ctx)
	if _, ok := wb.Options[extHidden]; ok {
		t.Fatal("non-exported package options must be hidden from TUI workbench")
	}
	if _, ok := wb.KConfigs[extHidden]; ok {
		t.Fatal("non-exported package KConfigs must be hidden from TUI workbench")
	}
	if _, ok := wb.Deps[extHidden]; ok {
		t.Fatal("non-exported package must be hidden from TUI workbench Deps")
	}
	for _, dep := range wb.Deps[extFacade] {
		if dep == extHidden {
			t.Fatal("non-exported dependency edge must be hidden from TUI workbench Deps")
		}
	}
	if _, ok := wb.DerivedOptions[extHidden]; ok {
		t.Fatal("non-exported package DerivedOptions must be hidden from TUI workbench")
	}
	if _, ok := wb.DerivedOptions[extFacade]["secret_knob"]; ok {
		t.Fatal("internal option DerivedOptions must be hidden from TUI workbench")
	}
	if _, ok := wb.Options[extFacade]["fps"]; !ok {
		t.Fatal("exported package options must remain visible in TUI workbench")
	}
	ctx.Config.SetOption(extHidden, "raw_knob", true)
	if err := ValidateConfigValues(ctx, NeededSet{localApp: true}); err == nil || !strings.Contains(err.Error(), "not exported") {
		t.Fatalf("ValidateConfigValues(inactive non-exported) = %v, want not exported error", err)
	}
	ctx.Config.UnsetOption(extHidden, "raw_knob")
	ctx.Config.SetOption(extFacade, "secret_knob", true)
	if err := ValidateConfigValues(ctx, NeededSet{localApp: true}); err == nil || !strings.Contains(err.Error(), "internal") {
		t.Fatalf("ValidateConfigValues(inactive internal option) = %v, want internal error", err)
	}
	ctx.Config.UnsetOption(extFacade, "secret_knob")

	facadePkg.SetOptions(ctx.allOptions[extFacade])
	facadePkg.SetCfgVals(map[string]any{"fps": true, "secret_knob": true})
	appPkg.SetOptions(map[string]*api.Option{})
	appPkg.SetCfgVals(map[string]any{})
	bctxImport := api.NewBuildContext(localApp, nil)
	bctxImport.ImportConfig(extFacade)
	bctxImport.GenerateConfigDefines()
	bctxImport.Target("app").SetKind(api.TargetBinary)
	applyBuildContextConfig(bctxImport, r.Graph().Packages[localApp], ctx, localApp, nil)
	for _, d := range bctxImport.GetTargets()["app"].Defines() {
		if strings.Contains(d, "SECRET_KNOB") {
			t.Fatalf("ImportConfig leaked cross-container internal option define %q", d)
		}
	}

	meta := map[string]build.PkgBuildMeta{
		localApp:  pkgBuildMetaFor(ctx, r.Graph().Packages[localApp], "@local/app", false),
		extFacade: pkgBuildMetaFor(ctx, r.Graph().Packages[extFacade], extFacade, false),
		extHidden: pkgBuildMetaFor(ctx, r.Graph().Packages[extHidden], extHidden, false),
	}
	bctxApp := api.NewBuildContext(localApp, nil)
	bctxApp.Target("app").SetKind(api.TargetBinary).AddDeps(extHidden)
	bctxFacade := api.NewBuildContext(extFacade, nil)
	bctxFacade.Target("graphics").SetKind(api.TargetVoid).AddDeps("//system/freertos/lvgl")
	bctxHidden := api.NewBuildContext(extHidden, nil)
	bctxHidden.Target("lvgl").SetKind(api.TargetStatic)
	if _, err := build.NewBuildGraph(map[string]map[string]*api.Target{
		localApp:  bctxApp.GetTargets(),
		extFacade: bctxFacade.GetTargets(),
		extHidden: bctxHidden.GetTargets(),
	}, meta); err == nil || !strings.Contains(err.Error(), "non-exported") {
		t.Fatalf("NewBuildGraph external dep on non-exported = %v, want non-exported error", err)
	}
	bctxAppOK := api.NewBuildContext(localApp, nil)
	bctxAppOK.Target("app").SetKind(api.TargetBinary).AddDeps(extFacade)
	if _, err := build.NewBuildGraph(map[string]map[string]*api.Target{
		localApp:  bctxAppOK.GetTargets(),
		extFacade: bctxFacade.GetTargets(),
		extHidden: bctxHidden.GetTargets(),
	}, meta); err != nil {
		t.Fatalf("NewBuildGraph via exported facade failed: %v", err)
	}
}
