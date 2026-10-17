package tui

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestNewModelDescriptionAndDisplayDefaults(t *testing.T) {
	values := map[string]map[string]any{}
	globals := map[string]any{api.ToolchainOptionName: "host"}
	called := ""
	m := newModel(modelInput{
		packages:     mkSources("app"),
		values:       values,
		globalValues: globals,
		workDir:      "/w",
		description:  "desc",
		displayDefaults: func(name string) map[string]any {
			called = name
			return map[string]any{"x": "y"}
		},
	})
	if m.description != "desc" || m.origDescription != "desc" {
		t.Fatalf("description = %q / %q, want desc/desc", m.description, m.origDescription)
	}
	if m.hasChanges {
		t.Error("construction should not report changes")
	}
	if got := m.result().Description; got != "desc" {
		t.Errorf("result description = %q, want desc", got)
	}
	if got := m.displayFallback("x"); got != "y" {
		t.Errorf("display fallback = %v, want y", got)
	}
	if called != "host" {
		t.Errorf("displayDefaults called with %q, want host", called)
	}
}

func TestModelResult(t *testing.T) {
	m := NewModel(mkSources("app"), nil, nil, map[string]map[string]any{}, "/w", "gcc", nil, nil, nil)
	m.saved = true
	m.description = "desc"
	m.menuconfigRan["app"] = true
	m.presetValues["app"] = "default"

	result := m.result()
	if !result.Saved || result.Description != "desc" {
		t.Fatalf("unexpected result metadata: %+v", result)
	}
	if result.Toolchain != "gcc" {
		t.Errorf("Toolchain = %q, want gcc", result.Toolchain)
	}
	if result.Values == nil || result.GlobalValues == nil {
		t.Fatal("result should expose the model maps")
	}
	if !result.MenuconfigRan["app"] || result.PresetValues["app"] != "default" {
		t.Errorf("unexpected kconfig result: %+v", result)
	}
}
