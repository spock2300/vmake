package tui

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestResolveInitialStateDerivesChipToolchainAndDefaults(t *testing.T) {
	options := map[string]*api.Option{
		"mode":                  mkOpt("mode", api.OptionChoice, "debug").SetValues("debug", "release"),
		api.ChipOptionName:      mkOpt(api.ChipOptionName, api.OptionChoice, "chip-a").SetValues("chip-a"),
		api.ToolchainOptionName: mkOpt(api.ToolchainOptionName, api.OptionChoice, "tc-a").SetValues("tc-a"),
	}
	state := resolveInitialState(Input{
		Options:       map[string]map[string]*api.Option{"app": options},
		GlobalOptions: options,
		GlobalValues:  map[string]any{},
		ChipProfiles: map[string]api.ChipProfile{
			"chip-a": {Name: "chip-a", PreferredToolchain: "tc-a", TargetOS: "none", TargetTriple: "arm"},
		},
	})
	if got := state.globalValues[api.ToolchainOptionName]; got != "tc-a" {
		t.Fatalf("derived toolchain = %v, want tc-a", got)
	}
	if !state.derivedGlobals[api.ToolchainOptionName] ||
		!state.derivedGlobals[api.TargetOSOptionName] ||
		!state.derivedGlobals[api.TargetTripleOptionName] {
		t.Fatalf("derived flags = %v", state.derivedGlobals)
	}
	if got := state.globalValues["mode"]; got != "debug" {
		t.Fatalf("mode = %v, want debug", got)
	}
}

func TestResolveInitialStatePreservesExplicitToolchainAndInputMaps(t *testing.T) {
	globalOptions := map[string]*api.Option{
		"mode":                  mkOpt("mode", api.OptionChoice, api.ModeDebug).SetValues(api.ModeDebug, api.ModeRelease),
		api.ToolchainOptionName: mkOpt(api.ToolchainOptionName, api.OptionChoice, "tc-a").SetValues("tc-a"),
	}
	packageOptions := map[string]map[string]*api.Option{
		"app": {
			api.ToolchainOptionName: mkOpt(api.ToolchainOptionName, api.OptionChoice, "tc-a").SetValues("tc-a").SetGroup(api.GroupGlobal),
			"local":                 mkOpt("local", api.OptionBool, true),
		},
	}
	globalValues := map[string]any{api.ToolchainOptionName: "explicit"}
	values := map[string]map[string]any{"app": {api.ToolchainOptionName: "stale"}}
	state := resolveInitialState(Input{
		Options:       packageOptions,
		GlobalOptions: globalOptions,
		GlobalValues:  globalValues,
		Values:        values,
	})
	if got := state.globalValues[api.ToolchainOptionName]; got != "explicit" {
		t.Fatalf("explicit toolchain = %v, want explicit", got)
	}
	if state.derivedGlobals[api.ToolchainOptionName] {
		t.Fatal("explicit toolchain must not be marked derived")
	}
	if got := state.globalValues["mode"]; got != api.ModeDebug {
		t.Fatalf("mode = %v, want %s", got, api.ModeDebug)
	}
	if _, ok := state.values["app"][api.ToolchainOptionName]; ok {
		t.Fatal("global option value must be removed from package values")
	}
	if got := state.values["app"]["local"]; got != true {
		t.Fatalf("local default = %v, want true", got)
	}

	if got := len(globalValues); got != 1 {
		t.Fatalf("resolveInitialState mutated caller global values: %v", globalValues)
	}
	if got := globalValues[api.ToolchainOptionName]; got != "explicit" {
		t.Fatalf("caller toolchain = %v, want explicit", got)
	}
	if _, ok := values["app"][api.ToolchainOptionName]; !ok {
		t.Fatal("resolveInitialState mutated caller package values")
	}
	if got := len(values["app"]); got != 1 {
		t.Fatalf("resolveInitialState mutated caller package values: %v", values["app"])
	}
}
