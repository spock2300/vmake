package tui

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestNewModelDescriptionAndDisplayDefaults(t *testing.T) {
	values := map[string]map[string]any{}
	globals := map[string]any{api.ToolchainOptionName: "host"}
	called := ""
	m := NewModel(Input{
		Packages:     mkSources("app"),
		Values:       values,
		GlobalValues: globals,
		WorkDir:      "/w",
		Description:  "desc",
		DisplayDefaults: func(name string) map[string]any {
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
	m := testModel(mkSources("app"), nil, nil, map[string]map[string]any{}, "/w", "gcc", nil, nil, nil)
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

func TestModelResultSparseExplicitAndDerivedDefaults(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {
			"demo":  mkOpt("demo", api.OptionChoice, "widgets").SetValues("widgets", "music"),
			"debug": mkOpt("debug", api.OptionBool, false),
		},
		"board": {
			"graphics": mkOpt("graphics", api.OptionBool, false),
			"sdcard":   mkOpt("sdcard", api.OptionBool, false),
		},
	}
	globalOpts := map[string]*api.Option{
		"mode": mkOpt("mode", api.OptionChoice, "release").SetValues("debug", "release"),
	}
	m := NewModel(Input{
		Packages:      mkSources("app", "board"),
		Deps:          map[string][]string{"app": {"board"}},
		Options:       opts,
		Values:        map[string]map[string]any{"app": {"demo": "music"}},
		DerivedOptions: map[string]map[string]any{"board": {"graphics": true}},
		GlobalOptions: globalOpts,
		GlobalValues:  map[string]any{},
		WorkDir:       "/w",
	})

	m.selectedPkg = "board"
	m.buildOptionItems()
	if got := m.getValue("graphics"); got != true {
		t.Fatalf("derived graphics display = %v, want true", got)
	}
	if got := m.defaultFor("graphics"); got != true {
		t.Fatalf("derived graphics defaultFor = %v, want true", got)
	}

	res := m.result()
	if len(res.Values) != 1 || res.Values["app"]["demo"] != "music" {
		t.Fatalf("initial sparse result should only contain explicit app/demo, got %v", res.Values)
	}
	if _, ok := res.Values["board"]; ok {
		t.Fatalf("untouched derived/default board options must not be saved, got %v", res.Values)
	}
	if len(res.GlobalValues) != 0 {
		t.Fatalf("untouched global defaults must not be saved, got %v", res.GlobalValues)
	}

	m.setValue("sdcard", true)
	res = m.result()
	if res.Values["board"]["sdcard"] != true {
		t.Fatalf("edited board/sdcard should become explicit, got %v", res.Values)
	}

	m.selectedPkg = "app"
	m.buildOptionItems()
	m.resetOptionToDefault("demo")
	if !m.hasChanges || !m.isOptionModified("demo") {
		t.Fatal("resetting explicit option to default must mark model as modified")
	}
	res = m.result()
	if _, ok := res.Values["app"]; ok {
		t.Fatalf("reset-to-default should drop explicit app/demo from sparse result, got %v", res.Values)
	}

	m.resetOption("demo")
	res = m.result()
	if res.Values["app"]["demo"] != "music" {
		t.Fatalf("resetOption should restore explicit app/demo=music, got %v", res.Values)
	}
}
