package api

import (
	"reflect"
	"testing"
)

func TestOptionDefaults(t *testing.T) {
	o := &Option{name: "feature"}
	if o.Name() != "feature" {
		t.Errorf("Name = %q", o.Name())
	}
	if o.IsGlobal() {
		t.Error("new option should not be global")
	}
}

func TestOptionFluent(t *testing.T) {
	o := (&Option{name: "opt"}).
		SetType(OptionBool).
		SetDefault(true).
		SetDescription("desc").
		SetValues("a", "b").
		SetGroup("Global")

	if o.Type() != OptionBool {
		t.Errorf("Type = %v, want OptionBool", o.Type())
	}
	if o.Default() != true {
		t.Errorf("Default = %v, want true", o.Default())
	}
	if o.Description() != "desc" {
		t.Errorf("Description = %q", o.Description())
	}
	if !reflect.DeepEqual(o.Values(), []string{"a", "b"}) {
		t.Errorf("Values = %v", o.Values())
	}
	if !o.IsGlobal() {
		t.Error("option should be global after SetGroup(Global)")
	}
}

func TestOptionOnApplyStored(t *testing.T) {
	called := false
	fn := func(ctx *ConfigContext, val any) {
		called = true
	}
	o := (&Option{name: "x"}).SetOnApply(fn)
	if o.OnApply() == nil {
		t.Fatal("OnApply should not be nil")
	}
	o.OnApply()(nil, true)
	if !called {
		t.Error("OnApply callback not invoked")
	}
}

func TestOptionShowIfStored(t *testing.T) {
	predicate := func(ctx *ConfigContext) bool { return true }
	o := (&Option{name: "x"}).SetShowIf(predicate)
	if o.ShowIf() == nil {
		t.Fatal("ShowIf should not be nil")
	}
}

func TestOptionTypeString(t *testing.T) {
	tests := []struct {
		t    OptionType
		want string
	}{
		{OptionBool, "bool"},
		{OptionString, "string"},
		{OptionInt, "int"},
		{OptionChoice, "choice"},
		{OptionType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.t.String(); got != tt.want {
			t.Errorf("OptionType(%d).String() = %q, want %q", tt.t, got, tt.want)
		}
	}
}
