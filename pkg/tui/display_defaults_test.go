package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spock2300/vmake/pkg/api"
)

func newGlobalModel(t *testing.T, toolchainName string, configure func(ctx *api.ConfigContext)) *Model {
	t.Helper()
	cfg := api.NewConfigContext("root")
	configure(cfg)
	model := NewModel(mkSources("app"), nil, nil, map[string]map[string]any{}, "/w", toolchainName, cfg.GetOptions(), nil, nil)
	model.selectedPkg = GlobalPkgName
	model.buildOptionItems()
	return &model
}

func TestDisplayDefaultsFillUnsetGlobalOptions(t *testing.T) {
	m := newGlobalModel(t, "arm-toolchain", func(cfg *api.ConfigContext) {
		cfg.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString)
		cfg.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString)
	})
	m.displayDefaults = func(name string) map[string]any {
		if name != "arm-toolchain" {
			return nil
		}
		return map[string]any{api.TargetOSOptionName: "none", api.TargetTripleOptionName: "arm-none-eabi"}
	}

	if got := m.getValue(api.TargetOSOptionName); got != "none" {
		t.Errorf("displayed target_os = %v, want toolchain default", got)
	}
	if got := m.getValue(api.TargetTripleOptionName); got != "arm-none-eabi" {
		t.Errorf("displayed target_triple = %v, want toolchain default", got)
	}
	if m.isOptionModified(api.TargetTripleOptionName) {
		t.Error("toolchain-provided default must not count as modified")
	}
	if _, ok := m.globalValues[api.TargetTripleOptionName]; ok {
		t.Error("toolchain-provided default must not be stored as an explicit value")
	}

	m.setValue(api.ToolchainOptionName, "host")
	if got := m.getValue(api.TargetTripleOptionName); got != nil {
		t.Errorf("host toolchain should not inherit another toolchain's default: %v", got)
	}
	m.setValue(api.ToolchainOptionName, "arm-toolchain")
	if got := m.getValue(api.TargetTripleOptionName); got != "arm-none-eabi" {
		t.Errorf("fallback after toolchain switch = %v", got)
	}

	m.setValue(api.TargetTripleOptionName, "aarch64-none-elf")
	if got := m.getValue(api.TargetTripleOptionName); got != "aarch64-none-elf" {
		t.Errorf("explicit value = %v", got)
	}
	if !m.isOptionModified(api.TargetTripleOptionName) {
		t.Error("explicit value should count as modified")
	}
	m.setValue(api.ToolchainOptionName, "host")
	if got := m.getValue(api.TargetTripleOptionName); got != "aarch64-none-elf" {
		t.Errorf("explicit value should survive toolchain changes: %v", got)
	}
}

func TestDisplayDefaultsIgnoreStoredEmptyValues(t *testing.T) {
	cfg := api.NewConfigContext("root")
	cfg.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString)
	model := NewModel(mkSources("app"), nil, nil, map[string]map[string]any{}, "/w", "arm-toolchain", cfg.GetOptions(), map[string]any{api.TargetTripleOptionName: ""}, nil)
	model.selectedPkg = GlobalPkgName
	model.buildOptionItems()
	model.displayDefaults = func(name string) map[string]any {
		if name != "arm-toolchain" {
			return nil
		}
		return map[string]any{api.TargetTripleOptionName: "arm-none-eabi"}
	}
	if got := model.getValue(api.TargetTripleOptionName); got != "arm-none-eabi" {
		t.Errorf("stored empty value should display the toolchain default: %v", got)
	}
	if model.isOptionModified(api.TargetTripleOptionName) {
		t.Error("stored empty value must not count as modified")
	}
}

func TestDisplayDefaultsUnchangedEditIsNoOp(t *testing.T) {
	m := newGlobalModel(t, "arm-toolchain", func(cfg *api.ConfigContext) {
		cfg.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString)
	})
	m.displayDefaults = func(name string) map[string]any {
		if name != "arm-toolchain" {
			return nil
		}
		return map[string]any{api.TargetTripleOptionName: "arm-none-eabi"}
	}

	m.editing = true
	m.editInput = "arm-none-eabi"
	m.editCursor = len(m.editInput)
	m.handleEditKey(tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok := m.globalValues[api.TargetTripleOptionName]; ok {
		t.Error("confirming an unchanged placeholder must not store it")
	}

	m.editing = true
	m.editInput = "aarch64-none-elf"
	m.editCursor = len(m.editInput)
	m.handleEditKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.globalValues[api.TargetTripleOptionName]; got != "aarch64-none-elf" {
		t.Errorf("changed edit value = %v", got)
	}
}

func TestDisplayDefaultsResetPackageEmptyDefault(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"path": api.NewConfigContext("app").Option("path").SetType(api.OptionString).SetDefault("")},
	}
	values := map[string]map[string]any{"app": {"path": "/usr/local"}}
	model := NewModel(mkSources("app"), nil, opts, values, "/w", "", nil, nil, nil)
	model.selectedPkg = "app"
	model.buildOptionItems()
	model.resetOptionToDefault("path")
	if got := model.getValue("path"); got != "" {
		t.Errorf("package option with an empty default = %v, want empty", got)
	}
}

func TestDisplayDefaultsRespectDeclaredDefaults(t *testing.T) {
	m := newGlobalModel(t, "arm-toolchain", func(cfg *api.ConfigContext) {
		cfg.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString).SetDefault("riscv64-unknown-elf")
	})
	m.displayDefaults = func(string) map[string]any {
		return map[string]any{api.TargetTripleOptionName: "arm-none-eabi"}
	}
	if got := m.getValue(api.TargetTripleOptionName); got != "riscv64-unknown-elf" {
		t.Errorf("declared default should win: %v", got)
	}
	m.setValue(api.TargetTripleOptionName, "aarch64-none-elf")
	m.resetOptionToDefault(api.TargetTripleOptionName)
	if got := m.getValue(api.TargetTripleOptionName); got != "riscv64-unknown-elf" {
		t.Errorf("reset to declared default = %v", got)
	}
}

func TestDisplayDefaultsResetDoesNotPersistPlaceholder(t *testing.T) {
	m := newGlobalModel(t, "arm-toolchain", func(cfg *api.ConfigContext) {
		cfg.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString)
	})
	m.displayDefaults = func(name string) map[string]any {
		if name != "arm-toolchain" {
			return nil
		}
		return map[string]any{api.TargetTripleOptionName: "arm-none-eabi"}
	}

	m.resetOption(api.TargetTripleOptionName)
	if _, ok := m.globalValues[api.TargetTripleOptionName]; ok {
		t.Error("resetOption must not store the display fallback")
	}
	if m.hasChanges {
		t.Error("resetOption on an untouched option must not mark changes")
	}

	m.setValue(api.TargetTripleOptionName, "aarch64-none-elf")
	m.resetOptionToDefault(api.TargetTripleOptionName)
	if _, ok := m.globalValues[api.TargetTripleOptionName]; ok {
		t.Error("resetOptionToDefault must not store the display fallback")
	}
	if m.isOptionModified(api.TargetTripleOptionName) {
		t.Error("reset option must not count as modified")
	}
	if got := m.getValue(api.TargetTripleOptionName); got != "arm-none-eabi" {
		t.Errorf("displayed value after reset = %v", got)
	}
}
