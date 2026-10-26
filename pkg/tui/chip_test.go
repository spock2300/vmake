package tui

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func chipTestModel() Model {
	return chipTestModelWithValues(nil)
}

func chipTestModelWithValues(globalValues map[string]any) Model {
	globalOptions := map[string]*api.Option{
		api.ChipOptionName:         mkOpt(api.ChipOptionName, api.OptionChoice, "chip-a").SetValues("chip-a", "chip-b"),
		api.ToolchainOptionName:    mkOpt(api.ToolchainOptionName, api.OptionChoice, "tc-a").SetValues("tc-a", "tc-b"),
		api.TargetOSOptionName:     mkOpt(api.TargetOSOptionName, api.OptionString, ""),
		api.TargetTripleOptionName: mkOpt(api.TargetTripleOptionName, api.OptionString, ""),
	}
	profiles := map[string]api.ChipProfile{
		"chip-a": {Name: "chip-a", PreferredToolchain: "tc-a", TargetOS: "none", TargetTriple: "triple-a"},
		"chip-b": {Name: "chip-b", PreferredToolchain: "tc-b", TargetOS: "none", TargetTriple: "triple-b"},
	}
	if globalValues == nil {
		globalValues = map[string]any{}
	}
	m := newModel(modelInput{
		packages:         mkSources("app"),
		values:           map[string]map[string]any{},
		currentToolchain: "tc-a",
		globalOptions:    globalOptions,
		globalValues:     globalValues,
		chipProfiles:     profiles,
	})
	m.selectedPkg = GlobalPkgName
	return m
}

func TestChipSwitchUpdatesToolchainAndTargets(t *testing.T) {
	m := chipTestModel()
	if got := m.getValue(api.ToolchainOptionName); got != "tc-a" {
		t.Fatalf("initial toolchain = %v, want tc-a", got)
	}
	if !m.derivedGlobals[api.ToolchainOptionName] {
		t.Fatal("initial toolchain should be derived")
	}
	if got := m.displayFallback(api.TargetTripleOptionName); got != "triple-a" {
		t.Fatalf("initial triple = %v, want triple-a", got)
	}

	m.setValue(api.ChipOptionName, "chip-b")
	if got := m.getValue(api.ToolchainOptionName); got != "tc-b" {
		t.Fatalf("switched toolchain = %v, want tc-b", got)
	}
	if !m.derivedGlobals[api.ToolchainOptionName] {
		t.Fatal("switched toolchain should be derived")
	}
	if got := m.displayFallback(api.TargetTripleOptionName); got != "triple-b" {
		t.Fatalf("switched triple = %v, want triple-b", got)
	}
}

func TestChipSwitchPreservesExplicitTarget(t *testing.T) {
	m := chipTestModel()
	m.setValue(api.TargetTripleOptionName, "custom-triple")
	if m.derivedGlobals[api.TargetTripleOptionName] {
		t.Fatal("explicit target should not be derived")
	}
	m.setValue(api.ChipOptionName, "chip-b")
	if got := m.getValue(api.TargetTripleOptionName); got != "custom-triple" {
		t.Fatalf("explicit triple = %v, want custom-triple", got)
	}
}

func TestManualToolchainIsExplicit(t *testing.T) {
	m := chipTestModel()
	m.setValue(api.ToolchainOptionName, "tc-b")
	if m.derivedGlobals[api.ToolchainOptionName] {
		t.Fatal("manual toolchain should be explicit")
	}
	if got := m.result().Toolchain; got != "tc-b" {
		t.Fatalf("result toolchain = %q, want tc-b", got)
	}
	if m.result().DerivedGlobals[api.ToolchainOptionName] {
		t.Fatal("manual toolchain should not be derived in the result")
	}
}

func TestResetToolchainUsesChipProfile(t *testing.T) {
	m := chipTestModel()
	m.setValue(api.ChipOptionName, "chip-b")
	m.setValue(api.ToolchainOptionName, "tc-a")
	if m.derivedGlobals[api.ToolchainOptionName] {
		t.Fatal("manual toolchain should be explicit")
	}
	m.resetOptionToDefault(api.ToolchainOptionName)
	if got := m.getValue(api.ToolchainOptionName); got != "tc-b" {
		t.Fatalf("reset toolchain = %v, want tc-b", got)
	}
	if !m.derivedGlobals[api.ToolchainOptionName] {
		t.Fatal("reset toolchain should be derived")
	}
}

func TestExplicitToolchainSurvivesChipSwitch(t *testing.T) {
	m := chipTestModelWithValues(map[string]any{
		api.ToolchainOptionName: "tc-custom",
	})
	if m.derivedGlobals[api.ToolchainOptionName] {
		t.Fatal("configured toolchain should be explicit")
	}
	m.setValue(api.ChipOptionName, "chip-b")
	if got := m.getValue(api.ToolchainOptionName); got != "tc-custom" {
		t.Fatalf("explicit toolchain = %v, want tc-custom", got)
	}
	if m.derivedGlobals[api.ToolchainOptionName] {
		t.Fatal("explicit toolchain became derived")
	}
}

func TestConfiguredTargetsStayExplicit(t *testing.T) {
	m := chipTestModelWithValues(map[string]any{
		api.TargetOSOptionName:     "none",
		api.TargetTripleOptionName: "triple-a",
	})
	if m.derivedGlobals[api.TargetTripleOptionName] {
		t.Fatal("configured target triple should stay explicit")
	}
	m.setValue(api.ChipOptionName, "chip-b")
	if got := m.getValue(api.TargetTripleOptionName); got != "triple-a" {
		t.Fatalf("explicit triple = %v, want triple-a", got)
	}
}

func TestResetDerivedTargetAfterChipSwitch(t *testing.T) {
	m := chipTestModel()
	m.setValue(api.ChipOptionName, "chip-b")
	m.resetOption(api.TargetTripleOptionName)
	if _, ok := m.globalValues[api.TargetTripleOptionName]; ok {
		t.Fatal("derived target triple was stored")
	}
	if got := m.displayFallback(api.TargetTripleOptionName); got != "triple-b" {
		t.Fatalf("displayed triple = %v, want triple-b", got)
	}
}

func TestChipTargetDefaultsFollowToolchainTarget(t *testing.T) {
	m := chipTestModelWithValues(map[string]any{
		api.ToolchainOptionName: "tc-other",
	})
	m.displayDefaults = func(name string) map[string]any {
		if name != "tc-other" {
			return nil
		}
		return map[string]any{api.TargetTripleOptionName: "other-triple"}
	}
	if got := m.declaredDefault(api.TargetTripleOptionName); got != "" {
		t.Fatalf("chip target default = %v, want the option default", got)
	}
	if got := m.displayFallback(api.TargetTripleOptionName); got != "other-triple" {
		t.Fatalf("displayed triple = %v, want other-triple", got)
	}
}
