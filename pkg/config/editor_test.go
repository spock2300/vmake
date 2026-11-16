package config

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func mkOpt(name string, t api.OptionType, def any) *api.Option {
	return (&api.Option{}).SetType(t).SetDefault(def)
}

func TestHeadlessEditorSparseCommitAndChipCascade(t *testing.T) {
	globalOpts := map[string]*api.Option{
		api.ChipOptionName:         mkOpt(api.ChipOptionName, api.OptionChoice, "chip-a").SetValues("chip-a", "chip-b"),
		api.ToolchainOptionName:    mkOpt(api.ToolchainOptionName, api.OptionChoice, "tc-a").SetValues("tc-a", "tc-b"),
		api.ModeOptionName:         mkOpt(api.ModeOptionName, api.OptionChoice, "debug").SetValues("debug", "release"),
		api.TargetTripleOptionName: mkOpt(api.TargetTripleOptionName, api.OptionString, ""),
	}
	pkgOpts := map[string]map[string]*api.Option{
		"app": {
			"demo":  mkOpt("demo", api.OptionChoice, "widgets").SetValues("widgets", "music"),
			"debug": mkOpt("debug", api.OptionBool, false),
		},
		"board": {
			"graphics": mkOpt("graphics", api.OptionBool, false),
		},
	}
	profiles := map[string]api.ChipProfile{
		"chip-a": {Name: "chip-a", PreferredToolchain: "tc-a", TargetOS: "none", TargetTriple: "triple-a"},
		"chip-b": {Name: "chip-b", PreferredToolchain: "tc-b", TargetOS: "none", TargetTriple: "triple-b"},
	}

	e := NewEditor(EditorInput{
		Options:        pkgOpts,
		Values:         map[string]map[string]any{"app": {"demo": "music"}},
		DerivedOptions: map[string]map[string]any{"board": {"graphics": true}},
		GlobalOptions:  globalOpts,
		GlobalValues:   map[string]any{},
		ChipProfiles:   profiles,
		Description:    "init",
	})

	if e.HasChanges() {
		t.Fatal("initial editor should not report changes")
	}
	if got := e.Get("", api.ToolchainOptionName); got != "tc-a" {
		t.Fatalf("derived toolchain = %v, want tc-a", got)
	}
	if got := e.Get("board", "graphics"); got != true {
		t.Fatalf("derived board/graphics = %v, want true", got)
	}

	e.Set("", api.ChipOptionName, "chip-b")
	if got := e.Get("", api.ToolchainOptionName); got != "tc-b" {
		t.Fatalf("cascaded toolchain = %v, want tc-b", got)
	}
	if !e.DerivedGlobals[api.ToolchainOptionName] {
		t.Fatal("cascaded toolchain should remain derived")
	}

	base := newConfigFile()
	base.SetOption("app", "demo", "music")
	committed, err := e.Commit(base, nil)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if committed.Global == nil || committed.Global.Toolchain != "" {
		t.Fatalf("derived toolchain must not be persisted in Global.Toolchain: %+v", committed.Global)
	}
	if committed.Global.Options[api.ChipOptionName] != "chip-b" {
		t.Fatalf("explicit chip = %v, want chip-b", committed.Global.Options[api.ChipOptionName])
	}
	if _, ok := committed.Entries["board"]; ok {
		t.Fatalf("untouched derived board options must remain sparse: %+v", committed.Entries["board"])
	}
	if committed.Entries["app"] == nil || committed.Entries["app"].Options["demo"] != "music" {
		t.Fatalf("explicit app/demo lost: %+v", committed.Entries["app"])
	}
}

func TestConfigFileSetUnsetAndParseFlags(t *testing.T) {
	pkg, opt, val, err := ParseSetFlag("vendor/pkg/opt=true")
	if err != nil || pkg != "vendor/pkg" || opt != "opt" || val != "true" {
		t.Fatalf("ParseSetFlag = (%q, %q, %q, %v)", pkg, opt, val, err)
	}
	uPkg, uOpt, err := ParseUnsetFlag("vendor/pkg/opt")
	if err != nil || uPkg != "vendor/pkg" || uOpt != "opt" {
		t.Fatalf("ParseUnsetFlag = (%q, %q, %v)", uPkg, uOpt, err)
	}

	cfg := newConfigFile()
	cfg.SetOption("", "mode", "release")
	cfg.SetOption("app", "flag", true)
	if !cfg.HasExplicitOption("", "mode") || !cfg.HasExplicitOption("app", "flag") {
		t.Fatal("HasExplicitOption returned false for set option")
	}
	if !cfg.UnsetOption("", "mode") || !cfg.UnsetOption("app", "flag") {
		t.Fatal("UnsetOption returned false")
	}
	PruneEmptyEntries(cfg)
	if cfg.Global != nil || len(cfg.Entries) != 0 {
		t.Fatalf("expected pruned empty config, got Global=%+v Entries=%+v", cfg.Global, cfg.Entries)
	}
}

func TestValuesEqualAndDeepCopyHelpers(t *testing.T) {
	a := map[string]map[string]any{"app": {"thread_count": 4}}
	b := map[string]map[string]any{"app": {"thread_count": float64(4)}}
	if !valuesEqual(a, b) {
		t.Fatal("valuesEqual should treat int(4) and float64(4) as equal")
	}
	if valuesEqual(
		map[string]map[string]any{"p": {"b": true}},
		map[string]map[string]any{"p": {"b": false}},
	) {
		t.Fatal("differing bools should not be equal")
	}
	if valuesEqual(
		map[string]map[string]any{"p": {"x": 1}},
		map[string]map[string]any{"p": {"y": 1}},
	) {
		t.Fatal("different option keys should not be equal")
	}

	srcVals := map[string]map[string]any{"p": {"x": 1}}
	dstVals := deepCopyValues(srcVals)
	dstVals["p"]["x"] = 99
	if srcVals["p"]["x"] == 99 {
		t.Fatal("deepCopyValues should isolate original from mutations")
	}

	srcGlobal := map[string]any{"x": 1}
	dstGlobal := deepCopyGlobal(srcGlobal)
	dstGlobal["x"] = 99
	if srcGlobal["x"] == 99 {
		t.Fatal("deepCopyGlobal should isolate original from mutations")
	}
}
