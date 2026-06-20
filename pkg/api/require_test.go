package api

import (
	"reflect"
	"testing"
)

func TestParseRequire(t *testing.T) {
	tests := []struct {
		input      string
		name       string
		constraint string
	}{
		{"foo", "foo", ""},
		{"foo>=1.0", "foo", ">=1.0"},
		{"foo >1.0", "foo", ">1.0"},
		{"foo >= 1.0", "foo", ">= 1.0"},
		{"foo@1.2.3", "foo", "@1.2.3"},
		{"foo<2.0", "foo", "<2.0"},
		{"foo=1.0", "foo", "=1.0"},
		{"foo~1.2", "foo", "~1.2"},
		{"repo/foo", "repo/foo", ""},
		{"repo/foo>=1.0", "repo/foo", ">=1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, constraint := parseRequire(tt.input)
			if name != tt.name || constraint != tt.constraint {
				t.Errorf("parseRequire(%q) = (%q, %q), want (%q, %q)",
					tt.input, name, constraint, tt.name, tt.constraint)
			}
		})
	}
}

func TestRequiresAddIgnoresEmpty(t *testing.T) {
	var r Requires
	r.Add("", "a", "", "b")
	if len(r.Get()) != 2 {
		t.Errorf("got %d requires, want 2", len(r.Get()))
	}
}

func TestRequiresReset(t *testing.T) {
	var r Requires
	r.Add("a", "b")
	r.Reset()
	if len(r.Get()) != 0 {
		t.Errorf("after Reset, got %d requires, want 0", len(r.Get()))
	}
}

func TestRequiresAddViaContext(t *testing.T) {
	ctx := NewRequireContextForConfig(nil, nil, nil)
	ctx.AddRequires("foo>=1.0", "bar")
	got := ctx.GetRequires()
	want := []RequireInfo{
		{Name: "foo", Constraint: ">=1.0"},
		{Name: "bar", Constraint: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetRequires() = %v, want %v", got, want)
	}
}

func TestRequiresResetViaContext(t *testing.T) {
	ctx := NewRequireContextForConfig(nil, nil, nil)
	ctx.AddRequires("foo")
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
		ctx.AddRequires("triggered")
	}
	ctx := NewRequireContextForConfig(nil, nil, []RequireFunc{fn, fn})
	ctx.RunFuncs()
	if called != 2 {
		t.Errorf("RunFuncs called %d times, want 2", called)
	}
	if len(ctx.GetRequires()) != 2 {
		t.Errorf("expected 2 requires after RunFuncs, got %d", len(ctx.GetRequires()))
	}
}
