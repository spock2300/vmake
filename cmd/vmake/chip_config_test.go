package main

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/tui"
)

func TestSaveConfigResultClearsDerivedChipGlobals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	ctx := &RuntimeContext{
		ConfigPath: path,
		Config: &config.ConfigFile{
			Version: config.ConfigVersion,
			Global: &config.GlobalConfig{
				Toolchain: "old-toolchain",
				Options: map[string]any{
					api.TargetOSOptionName:     "old-os",
					api.TargetTripleOptionName: "old-triple",
					api.ChipOptionName:         "old-chip",
				},
			},
		},
	}
	result := &tui.ConfigResult{
		Saved:     true,
		Toolchain: "chip-toolchain",
		GlobalValues: map[string]any{
			api.ModeOptionName:         api.ModeRelease,
			api.ChipOptionName:         "chip-b",
			api.ToolchainOptionName:    "chip-toolchain",
			api.TargetOSOptionName:     "none",
			api.TargetTripleOptionName: "chip-triple",
		},
		DerivedGlobals: map[string]bool{
			api.ToolchainOptionName:    true,
			api.TargetOSOptionName:     true,
			api.TargetTripleOptionName: true,
		},
	}
	if err := saveConfigResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Global.Toolchain != "" {
		t.Errorf("derived toolchain was pinned: %q", saved.Global.Toolchain)
	}
	if _, ok := saved.Global.Options[api.TargetOSOptionName]; ok {
		t.Error("derived target_os was pinned")
	}
	if _, ok := saved.Global.Options[api.TargetTripleOptionName]; ok {
		t.Error("derived target_triple was pinned")
	}
	if got := saved.Global.Options[api.ChipOptionName]; got != "chip-b" {
		t.Errorf("chip = %v, want chip-b", got)
	}
}

func TestSaveConfigResultKeepsExplicitChipTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	ctx := &RuntimeContext{
		ConfigPath: path,
		Config: &config.ConfigFile{
			Version: config.ConfigVersion,
			Global:  &config.GlobalConfig{Options: map[string]any{}},
		},
	}
	result := &tui.ConfigResult{
		Saved: true,
		GlobalValues: map[string]any{
			api.ChipOptionName:         "chip-a",
			api.TargetOSOptionName:     "none",
			api.TargetTripleOptionName: "riscv32-unknown-elf",
		},
	}
	if err := saveConfigResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Global.Options[api.TargetOSOptionName]; got != "none" {
		t.Errorf("explicit target_os = %v", got)
	}
	if got := saved.Global.Options[api.TargetTripleOptionName]; got != "riscv32-unknown-elf" {
		t.Errorf("explicit target_triple = %v", got)
	}
}

func TestSaveConfigResultWritesExplicitToolchain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	ctx := &RuntimeContext{
		ConfigPath: path,
		Config: &config.ConfigFile{
			Version: config.ConfigVersion,
			Global:  &config.GlobalConfig{Options: map[string]any{}},
		},
	}
	result := &tui.ConfigResult{
		Saved:     true,
		Toolchain: "explicit-toolchain",
		GlobalValues: map[string]any{
			api.ToolchainOptionName: "explicit-toolchain",
			api.ChipOptionName:      "chip-a",
		},
	}
	if err := saveConfigResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Global.Toolchain != "explicit-toolchain" {
		t.Errorf("explicit toolchain = %q", saved.Global.Toolchain)
	}
}
