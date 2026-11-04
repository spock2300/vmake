package api

import (
	"errors"
	"reflect"
	"testing"
)

func crossOptionAccessor(t *testing.T) *ConfigAccessor {
	t.Helper()
	a := newStrictAccessor()
	a.SetConfigLookup(func(pkg, name string) (any, *Option, error) {
		if pkg != "firmware" {
			return nil, nil, errors.New("unknown package " + pkg)
		}
		switch name {
		case "graphics_accel":
			return true, (&Option{name: "graphics_accel"}).SetType(OptionBool).SetDefault(false), nil
		case "graphics_test":
			return "none", (&Option{name: "graphics_test"}).SetType(OptionChoice).
				SetDefault("none").SetValues("none", "basic"), nil
		case "count":
			return float64(3), (&Option{name: "count"}).SetType(OptionInt).SetDefault(0), nil
		case "global":
			return "x", (&Option{name: "global"}).SetType(OptionString).SetDefault("").SetGroup(GroupGlobal), nil
		}
		return nil, nil, nil
	})
	return a
}

func TestCrossPackageReads(t *testing.T) {
	a := crossOptionAccessor(t)

	if !a.Bool("firmware/graphics_accel") {
		t.Error("Bool cross read = false, want true")
	}
	if got := a.BoolStr("firmware/graphics_accel"); got != "ON" {
		t.Errorf("BoolStr cross read = %q, want ON", got)
	}
	if got := a.String("firmware/graphics_test"); got != "none" {
		t.Errorf("String cross read = %q, want none", got)
	}
	if got := a.Int("firmware/count"); got != 3 {
		t.Errorf("Int cross read = %d, want 3", got)
	}
	if got := a.String("firmware/global"); got != "x" {
		t.Errorf("global cross read = %q, want x", got)
	}
	if !a.When("firmware/graphics_accel", true) {
		t.Error("When cross read should match true")
	}
	if a.When("firmware/graphics_accel", false) {
		t.Error("When cross read should not match false")
	}
	if !a.WhenNot("firmware/graphics_accel", false) {
		t.Error("WhenNot cross read should be true for false")
	}
	if got := a.If("firmware/graphics_accel", "enabled"); !reflect.DeepEqual(got, []string{"enabled"}) {
		t.Errorf("If cross read = %v, want [enabled]", got)
	}
	if got := a.Select("firmware/graphics_test", map[string]string{"none": "N"}); !reflect.DeepEqual(got, []string{"N"}) {
		t.Errorf("Select cross read = %v, want [N]", got)
	}
}

func TestCrossPackageRemotePackageName(t *testing.T) {
	a := newStrictAccessor()
	a.SetConfigLookup(func(pkg, name string) (any, *Option, error) {
		if pkg != "official/firmware" || name != "graphics_accel" {
			return nil, nil, nil
		}
		return true, (&Option{name: "graphics_accel"}).SetType(OptionBool).SetDefault(false), nil
	})
	if !a.Bool("official/firmware/graphics_accel") {
		t.Error("remote package cross read = false, want true")
	}
}

func TestCrossPackageReadErrors(t *testing.T) {
	t.Run("lookup error", func(t *testing.T) {
		a := newStrictAccessor()
		a.SetConfigLookup(func(string, string) (any, *Option, error) {
			return nil, nil, errors.New("boom")
		})
		expectBuildScriptError(t, func() { a.Bool("firmware/x") }, "Bool")
	})

	t.Run("unknown option", func(t *testing.T) {
		a := newStrictAccessor()
		a.SetConfigLookup(func(string, string) (any, *Option, error) {
			return nil, nil, nil
		})
		expectBuildScriptError(t, func() { a.Bool("firmware/x") }, "Bool")
	})

	t.Run("type mismatch", func(t *testing.T) {
		a := crossOptionAccessor(t)
		expectBuildScriptError(t, func() { a.String("firmware/graphics_accel") }, "String")
	})

	t.Run("malformed package", func(t *testing.T) {
		a := crossOptionAccessor(t)
		expectBuildScriptError(t, func() { a.Bool("/graphics_accel") }, "Bool")
	})

	t.Run("malformed option", func(t *testing.T) {
		a := crossOptionAccessor(t)
		expectBuildScriptError(t, func() { a.Bool("firmware/") }, "Bool")
	})
}

func TestCrossPackageDiscoveryRejected(t *testing.T) {
	a := ConfigAccessor{Options: map[string]*Option{}}
	a.setStrictOwner("strictpkg")
	a.discover = true

	expectBuildScriptError(t, func() { a.When("firmware/x", true) }, "When")
	expectBuildScriptError(t, func() { a.If("firmware/x", "v") }, "If")
	expectBuildScriptError(t, func() { a.Select("firmware/x", map[string]string{"a": "b"}) }, "Select")
}

func TestCrossPackageLenientContext(t *testing.T) {
	ctx := NewConfigContext("strictpkg").SetLenient()
	if ctx.Bool("firmware/graphics_accel") {
		t.Error("lenient cross Bool = true, want false")
	}
	if got := ctx.String("firmware/graphics_test"); got != "" {
		t.Errorf("lenient cross String = %q, want empty", got)
	}
	if got := ctx.Int("firmware/count"); got != 0 {
		t.Errorf("lenient cross Int = %d, want 0", got)
	}
	if ctx.When("firmware/graphics_accel", true) {
		t.Error("lenient cross When = true, want false")
	}
	if got := ctx.If("firmware/graphics_accel", "v"); got != nil {
		t.Errorf("lenient cross If = %v, want nil", got)
	}
	if got := ctx.Select("firmware/graphics_test", map[string]string{"none": "N"}); got != nil {
		t.Errorf("lenient cross Select = %v, want nil", got)
	}
}

func TestCrossPackageSlashNameRejected(t *testing.T) {
	a := newStrictAccessor()
	expectBuildScriptError(t, func() { a.Option("a/b") }, "Option")

	ctx := NewConfigContext("strictpkg")
	expectBuildScriptError(t, func() { ctx.GlobalOption("a/b") }, "Option")
	expectBuildScriptError(t, func() { ctx.SetConfigValue("a/b", 1) }, "SetConfigValue")
}

func TestValidateOptionRejectsSlash(t *testing.T) {
	if err := ValidateOption(&Option{name: "a/b"}); err == nil {
		t.Error("ValidateOption should reject option names containing '/'")
	}
}
