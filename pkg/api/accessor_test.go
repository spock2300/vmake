package api

import (
	"reflect"
	"testing"
)

func TestAccessorBool(t *testing.T) {
	tests := []struct {
		name string
		vals map[string]any
		opts map[string]*Option
		want bool
	}{
		{"unset, no option", map[string]any{}, map[string]*Option{}, false},
		{"value true", map[string]any{"x": true}, map[string]*Option{}, true},
		{"value false", map[string]any{"x": false}, map[string]*Option{}, false},
		{"falls back to default", map[string]any{}, map[string]*Option{"x": {name: "x", defaultVal: true}}, true},
		{"explicit value overrides default", map[string]any{"x": false}, map[string]*Option{"x": {name: "x", defaultVal: true}}, false},
		{"non-bool value ignored, falls to default", map[string]any{"x": "yes"}, map[string]*Option{"x": {name: "x", defaultVal: true}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewConfigAccessor(tt.vals, tt.opts)
			if got := a.Bool("x"); got != tt.want {
				t.Errorf("Bool(x) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccessorString(t *testing.T) {
	a := NewConfigAccessor(map[string]any{"name": "foo"}, nil)
	if got := a.String("name"); got != "foo" {
		t.Errorf("String(name) = %q, want foo", got)
	}
	if got := a.String("missing"); got != "" {
		t.Errorf("String(missing) = %q, want empty", got)
	}

	a2 := NewConfigAccessor(nil, map[string]*Option{"mode": {name: "mode", defaultVal: "debug"}})
	if got := a2.String("mode"); got != "debug" {
		t.Errorf("default fallback = %q, want debug", got)
	}
}

func TestAccessorIntCoercion(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want int
	}{
		{"int", 42, 42},
		{"int64", int64(99), 99},
		{"float64 from json", float64(7), 7},
		{"string ignored", "12", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewConfigAccessor(map[string]any{"n": tt.val}, nil)
			if got := a.Int("n"); got != tt.want {
				t.Errorf("Int(n) = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAccessorIntFallsBackToDefault(t *testing.T) {
	a := NewConfigAccessor(nil, map[string]*Option{"size": {name: "size", defaultVal: 256}})
	if got := a.Int("size"); got != 256 {
		t.Errorf("Int(size) default = %d, want 256", got)
	}
}

func TestAccessorBoolStr(t *testing.T) {
	a := NewConfigAccessor(map[string]any{"on": true, "off": false}, nil)
	if got := a.BoolStr("on"); got != "ON" {
		t.Errorf("BoolStr(on) = %q, want ON", got)
	}
	if got := a.BoolStr("off"); got != "OFF" {
		t.Errorf("BoolStr(off) = %q, want OFF", got)
	}
}

func TestAccessorIf(t *testing.T) {
	a := NewConfigAccessor(map[string]any{"debug": true, "release": false}, nil)
	if got := a.If("debug", "-g"); !reflect.DeepEqual(got, []string{"-g"}) {
		t.Errorf("If(debug) = %v", got)
	}
	if got := a.If("release", "-O3"); got != nil {
		t.Errorf("If(release) = %v, want nil", got)
	}
	if got := a.IfNot("release", "-O0"); !reflect.DeepEqual(got, []string{"-O0"}) {
		t.Errorf("IfNot(release) = %v", got)
	}
}

func TestAccessorIfDiscoverMode(t *testing.T) {
	a := NewConfigAccessor(nil, nil)
	a.CfgVals = nil
	got := a.If("anything", "-flag")
	if !reflect.DeepEqual(got, []string{"-flag"}) {
		t.Errorf("discover-mode If should pass through, got %v", got)
	}
}

func TestAccessorEqual(t *testing.T) {
	a := NewConfigAccessor(map[string]any{"mode": "debug"}, nil)
	if got := a.Equal("mode", "debug", "-g"); got != "-g" {
		t.Errorf("Equal(matching) = %q, want -g", got)
	}
	if got := a.Equal("mode", "release", "-O3"); got != "" {
		t.Errorf("Equal(non-matching) = %q, want empty", got)
	}
}

func TestAccessorEqualDiscoverMode(t *testing.T) {
	a := NewConfigAccessor(nil, nil)
	a.CfgVals = nil
	if got := a.Equal("anything", "anything", "dep"); got != "dep" {
		t.Errorf("discover-mode Equal should return dep, got %q", got)
	}
}

func TestAccessorSelect(t *testing.T) {
	a := NewConfigAccessor(map[string]any{"arch": "arm"}, nil)
	mapping := map[string]string{"arm": "-marm", "x86": "-mx86"}
	if got := a.Select("arch", mapping); got != "-marm" {
		t.Errorf("Select(arm) = %q", got)
	}
	if got := a.Select("missing", mapping); got != "" {
		t.Errorf("Select(missing) = %q, want empty", got)
	}
}

func TestAccessorSelectDiscoverMode(t *testing.T) {
	a := NewConfigAccessor(nil, nil)
	if got := a.Select("any", map[string]string{"a": "b"}); got != "" {
		t.Errorf("discover-mode Select should return empty, got %q", got)
	}
}

func TestAccessorWhen(t *testing.T) {
	a := NewConfigAccessor(map[string]any{"mode": "debug"}, nil)
	if !a.When("mode", "debug") {
		t.Error("When(matching) should be true")
	}
	if a.When("mode", "release") {
		t.Error("When(non-matching) should be false")
	}
}

func TestAccessorWhenDiscoverMode(t *testing.T) {
	a := NewConfigAccessor(nil, nil)
	a.CfgVals = nil
	if !a.When("anything", "anything") {
		t.Error("discover-mode When should always be true")
	}
}

func TestAccessorOptionLazilyCreates(t *testing.T) {
	a := NewConfigAccessor(nil, nil)
	opt := a.Option("new-opt")
	if opt == nil {
		t.Fatal("Option() returned nil")
	}
	if opt.Name() != "new-opt" {
		t.Errorf("Name = %q", opt.Name())
	}
	opt2 := a.Option("new-opt")
	if opt != opt2 {
		t.Error("Option(name) should return same pointer for same name")
	}
}

func TestAccessorMergeGlobalsNoOverwrite(t *testing.T) {
	localOpt := &Option{name: "x", defaultVal: "local"}
	a := NewConfigAccessor(
		map[string]any{"x": "local-val", "y": "local-y"},
		map[string]*Option{"x": localOpt},
	)
	globalOpt := &Option{name: "x", defaultVal: "global"}
	a.MergeGlobals(
		map[string]*Option{"x": globalOpt, "z": globalOpt},
		map[string]any{"x": "global-val", "w": "global-w"},
	)

	if a.Options["x"] != localOpt {
		t.Error("MergeGlobals should NOT overwrite existing local option")
	}
	if a.Options["z"] != globalOpt {
		t.Error("MergeGlobals should add new global option")
	}
	if a.CfgVals["x"] != "local-val" {
		t.Error("MergeGlobals should NOT overwrite existing local value")
	}
	if a.CfgVals["w"] != "global-w" {
		t.Error("MergeGlobals should add new global value")
	}
	if a.CfgVals["y"] != "local-y" {
		t.Error("MergeGlobals should preserve other local values")
	}
}
