package build

import (
	"testing"
)

func TestBuildKeyDeterministic(t *testing.T) {
	opts := map[string]any{"a": 1, "b": "x"}
	k1 := BuildKey("/usr/bin/gcc", "debug", opts)
	k2 := BuildKey("/usr/bin/gcc", "debug", opts)
	if k1 != k2 {
		t.Errorf("BuildKey not deterministic: %q vs %q", k1, k2)
	}
}

func TestBuildKeyDiffersOnToolchain(t *testing.T) {
	opts := map[string]any{"a": 1}
	k1 := BuildKey("/usr/bin/gcc", "debug", opts)
	k2 := BuildKey("/usr/bin/arm-gcc", "debug", opts)
	if k1 == k2 {
		t.Error("BuildKey should differ when toolchain differs")
	}
}

func TestBuildKeyDiffersOnMode(t *testing.T) {
	opts := map[string]any{"a": 1}
	k1 := BuildKey("gcc", "debug", opts)
	k2 := BuildKey("gcc", "release", opts)
	if k1 == k2 {
		t.Error("BuildKey should differ when mode differs")
	}
}

func TestBuildKeyDiffersOnOptions(t *testing.T) {
	k1 := BuildKey("gcc", "debug", map[string]any{"a": 1})
	k2 := BuildKey("gcc", "debug", map[string]any{"a": 2})
	if k1 == k2 {
		t.Error("BuildKey should differ when option value differs")
	}
}

func TestBuildKeyOrderIndependent(t *testing.T) {
	k1 := BuildKey("gcc", "debug", map[string]any{"a": 1, "b": 2})
	k2 := BuildKey("gcc", "debug", map[string]any{"b": 2, "a": 1})
	if k1 != k2 {
		t.Errorf("BuildKey should be order-independent for options: %q vs %q", k1, k2)
	}
}

func TestBuildKeyEmpty(t *testing.T) {
	k := BuildKey("", "", nil)
	if k == "" {
		t.Error("BuildKey should produce non-empty hash for empty input")
	}
}

func TestBuildCompileArgsBasic(t *testing.T) {
	opts := &CompileOptions{
		Includes: []string{"/inc1", "/inc2"},
		Defines:  []string{"FOO=1", "BAR"},
	}
	args := BuildCompileArgs(opts, "obj.o", "src.c", []string{"-Wall", "-O2"}, "dep.d")

	want := []string{"-c", "-MMD", "-MP", "-MF", "dep.d", "-o", "obj.o",
		"-I/inc1", "-I/inc2", "-DFOO=1", "-DBAR", "-Wall", "-O2", "src.c"}
	if !sliceEqual(args, want) {
		t.Errorf("BuildCompileArgs = %v\nwant %v", args, want)
	}
}

func TestBuildCompileArgsNoDepFile(t *testing.T) {
	opts := &CompileOptions{}
	args := BuildCompileArgs(opts, "o", "s", nil, "")
	for _, a := range args {
		if a == "-MMD" || a == "-MP" || a == "-MF" {
			t.Errorf("dep file args should be omitted when depPath empty, got %v", args)
		}
	}
}

func TestLinkPolicyVersionScriptFlag(t *testing.T) {
	p := LinkPolicy{VersionScript: "/path/export.map"}
	got := p.versionScriptFlag()
	want := "-Wl,--version-script=/path/export.map"
	if got != want {
		t.Errorf("versionScriptFlag = %q, want %q", got, want)
	}
}

func TestLinkPolicyVersionScriptFlagEmpty(t *testing.T) {
	p := LinkPolicy{}
	if got := p.versionScriptFlag(); got != "" {
		t.Errorf("empty VersionScript should produce empty flag, got %q", got)
	}
}

func TestLinkPolicyExcludeLibsFlag(t *testing.T) {
	p := LinkPolicy{ExcludeLibs: []string{"libfoo", "libbar"}}
	got := p.excludeLibsFlag()
	want := "-Wl,--exclude-libs=libfoo,libbar"
	if got != want {
		t.Errorf("excludeLibsFlag = %q, want %q", got, want)
	}
}

func TestLinkPolicyExcludeLibsFlagEmpty(t *testing.T) {
	p := LinkPolicy{}
	if got := p.excludeLibsFlag(); got != "" {
		t.Errorf("empty ExcludeLibs should produce empty, got %q", got)
	}
}

func TestLinkPolicyBindingFlags(t *testing.T) {
	tests := []struct {
		mode string
		want []string
	}{
		{"static", []string{"-Wl,-Bsymbolic"}},
		{"static-functions", []string{"-Wl,-Bsymbolic-functions"}},
		{"", nil},
		{"invalid", nil},
	}
	for _, tt := range tests {
		p := LinkPolicy{SymbolBinding: tt.mode}
		got := p.bindingFlags()
		if !sliceEqual(got, tt.want) {
			t.Errorf("bindingFlags(%q) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestNewLinker(t *testing.T) {
	tools := &ResolvedTools{CC: "/usr/bin/gcc", AR: "/usr/bin/ar"}
	l := NewLinker(tools)
	if l == nil {
		t.Fatal("NewLinker returned nil")
	}
}

func TestNewCompiler(t *testing.T) {
	tools := &ResolvedTools{CC: "/usr/bin/gcc", CXX: "/usr/bin/g++"}
	c := NewCompiler(tools)
	if c == nil {
		t.Fatal("NewCompiler returned nil")
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
