package api

import (
	"reflect"
	"testing"
)

func TestRequiresAddIgnoresEmpty(t *testing.T) {
	var r Requires
	r.Add("", "//a", "", "//b")
	if len(r.Get()) != 2 {
		t.Errorf("got %d requires, want 2", len(r.Get()))
	}
}

func TestRequiresReset(t *testing.T) {
	var r Requires
	r.Add("//a", "//b")
	r.Reset()
	if len(r.Get()) != 0 {
		t.Errorf("after Reset, got %d requires, want 0", len(r.Get()))
	}
}

func TestRequiresAddViaContext(t *testing.T) {
	ctx := NewRequireContextForConfig("strictpkg", nil, nil, nil)
	ctx.AddRequires("//foo", "hl/sdk/bar")
	got := ctx.GetRequires()
	want := []RequireInfo{
		{Name: "//foo"},
		{Name: "hl/sdk/bar"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetRequires() = %v, want %v", got, want)
	}
	expectBuildScriptError(t, func() { ctx.AddRequires("hl/sdk/bar >=1.0") }, "AddRequires")
	expectBuildScriptError(t, func() { ctx.AddRequires("bare") }, "AddRequires")
}

func TestRequiresResetViaContext(t *testing.T) {
	ctx := NewRequireContextForConfig("//app", nil, nil, nil)
	ctx.AddRequires("//foo")
	if len(ctx.GetRequires()) != 1 {
		t.Fatal("expected 1 require")
	}
	ctx.ResetRequires()
	if len(ctx.GetRequires()) != 0 {
		t.Errorf("after ResetRequires, got %d", len(ctx.GetRequires()))
	}
}

func TestRequireContextRunFuncs(t *testing.T) {
	called := 0
	fn := func(ctx *RequireContext) {
		called++
		ctx.AddRequires("//triggered")
	}
	ctx := NewRequireContextForConfig("//app", nil, nil, []RequireFunc{fn, fn})
	ctx.RunFuncs()
	if called != 2 {
		t.Errorf("RunFuncs called %d times, want 2", called)
	}
	if len(ctx.GetRequires()) != 2 {
		t.Errorf("expected 2 requires after RunFuncs, got %d", len(ctx.GetRequires()))
	}
}
