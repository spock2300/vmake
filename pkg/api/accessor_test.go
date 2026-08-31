package api

import (
	"reflect"
	"testing"
)

func expectBuildScriptError(t *testing.T, fn func(), wantOp string) {
	t.Helper()
	defer func() {
		r := recover()
		bse, ok := r.(*BuildScriptError)
		if !ok {
			t.Fatalf("expected *BuildScriptError, got %v", r)
		}
		if bse.Package != "strictpkg" {
			t.Errorf("package = %q, want strictpkg", bse.Package)
		}
		if wantOp != "" && bse.Op != wantOp {
			t.Errorf("op = %q, want %q", bse.Op, wantOp)
		}
	}()
	fn()
	t.Fatal("expected fatalScript panic")
}

func newStrictAccessor() *ConfigAccessor {
	a := NewConfigAccessor(nil, nil)
	a.setStrictOwner("strictpkg")
	return &a
}

func TestStrictAccessorUnknownOption(t *testing.T) {
	a := newStrictAccessor()
	expectBuildScriptError(t, func() { a.Bool("dbgu_enbaled") }, "Bool")
}

func TestStrictAccessorUnknownOptionString(t *testing.T) {
	a := newStrictAccessor()
	expectBuildScriptError(t, func() { a.String("nope") }, "String")
}

func TestStrictAccessorTypeMismatchOnValue(t *testing.T) {
	a := newStrictAccessor()
	a.Option("shared_lib").SetType(OptionBool).SetDefault(false)
	a.CfgVals["shared_lib"] = true
	expectBuildScriptError(t, func() { a.String("shared_lib") }, "String")
}

func TestStrictAccessorTypeMismatchEvenWithoutValue(t *testing.T) {
	a := newStrictAccessor()
	a.Option("shared_lib").SetType(OptionBool).SetDefault(false)
	expectBuildScriptError(t, func() { a.String("shared_lib") }, "String")
}

func TestStrictAccessorValueTypeMismatch(t *testing.T) {
	a := newStrictAccessor()
	a.Option("name").SetType(OptionString).SetDefault("x")
	a.CfgVals["name"] = 42
	expectBuildScriptError(t, func() { a.String("name") }, "String")
}

func TestStrictAccessorIntAcceptsFloat64(t *testing.T) {
	a := newStrictAccessor()
	a.Option("n").SetType(OptionInt).SetDefault(4)
	a.CfgVals["n"] = float64(7)
	if got := a.Int("n"); got != 7 {
		t.Errorf("Int = %d, want 7", got)
	}
}

func TestStrictAccessorDiscoverModeDirectReadFatals(t *testing.T) {
	a := ConfigAccessor{Options: map[string]*Option{}}
	a.setStrictOwner("strictpkg")
	a.discover = true
	expectBuildScriptError(t, func() { a.Bool("anything") }, "Bool")
}

func TestStrictAccessorDiscoverModeWhenAllowed(t *testing.T) {
	a := ConfigAccessor{Options: map[string]*Option{}}
	a.setStrictOwner("strictpkg")
	a.discover = true
	if !a.When("opt", "x") {
		t.Error("When in discover mode should return true")
	}
}

func TestStrictAccessorWhenUnknownOption(t *testing.T) {
	a := newStrictAccessor()
	a.CfgVals["known"] = "v"
	expectBuildScriptError(t, func() { a.When("typo", "v") }, "When")
}

func TestStrictAccessorWhenNumericCoercion(t *testing.T) {
	a := newStrictAccessor()
	a.Option("count").SetType(OptionInt).SetDefault(4)
	a.CfgVals["count"] = float64(4)
	if !a.When("count", 4) {
		t.Error("When(float64 4, int 4) should be true after numeric normalization")
	}
	if a.When("count", 5) {
		t.Error("When(4, 5) should be false")
	}
}

func TestNonStrictAccessorLenient(t *testing.T) {
	a := NewConfigAccessor(nil, nil)
	if a.Bool("unknown") {
		t.Error("non-strict Bool(unknown) should silently return false")
	}
	if a.String("unknown") != "" {
		t.Error("non-strict String(unknown) should silently return empty")
	}
}

func TestFlattenAnyPanicsOnUnsupportedType(t *testing.T) {
	defer func() {
		r := recover()
		bse, ok := r.(*BuildScriptError)
		if !ok {
			t.Fatalf("expected *BuildScriptError, got %v", r)
		}
		if bse.Op != "Add*" {
			t.Errorf("op = %q, want Add*", bse.Op)
		}
	}()
	flattenAny([]any{42})
	t.Fatal("expected fatalScript panic")
}

func TestValidateOptionDefaultTypeMismatch(t *testing.T) {
	o := &Option{name: "x"}
	o.SetDefault("hello")
	if err := ValidateOption(o); err == nil {
		t.Error("string default on untyped (bool-zero) option should fail validation")
	}

	o2 := &Option{name: "y"}
	o2.SetType(OptionChoice).SetDefault("beta").SetValues("alpha")
	if err := ValidateOption(o2); err == nil {
		t.Error("choice default outside SetValues should fail validation")
	}

	o3 := &Option{name: "z"}
	o3.SetType(OptionInt).SetDefault(3)
	if err := ValidateOption(o3); err != nil {
		t.Errorf("valid int default should pass: %v", err)
	}
}

func TestNormalizeOptionValueFloat64ToInt(t *testing.T) {
	o := &Option{name: "n"}
	o.SetType(OptionInt)
	got := NormalizeOptionValue(o, float64(9))
	if !reflect.DeepEqual(got, 9) {
		t.Errorf("NormalizeOptionValue = %#v, want int 9", got)
	}
}
