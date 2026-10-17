package build

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestBuildKeyERejectsUnsupportedOptions(t *testing.T) {
	if _, err := BuildKeyE("cc", "debug", map[string]any{"bad": make(chan int)}, ""); err == nil {
		t.Fatal("BuildKeyE accepted a non-JSON option value")
	}
}

func TestBuildKeyDeterministic(t *testing.T) {
	opts := map[string]any{"a": 1, "b": "x"}
	k1 := BuildKey("/usr/bin/gcc", "debug", opts, "")
	k2 := BuildKey("/usr/bin/gcc", "debug", opts, "")
	if k1 != k2 {
		t.Errorf("BuildKey not deterministic: %q vs %q", k1, k2)
	}
}

func TestBuildKeyDiffersOnToolchain(t *testing.T) {
	opts := map[string]any{"a": 1}
	k1 := BuildKey("/usr/bin/gcc", "debug", opts, "")
	k2 := BuildKey("/usr/bin/arm-gcc", "debug", opts, "")
	if k1 == k2 {
		t.Error("BuildKey should differ when toolchain differs")
	}
}

func TestBuildKeyDiffersOnMode(t *testing.T) {
	opts := map[string]any{"a": 1}
	k1 := BuildKey("gcc", "debug", opts, "")
	k2 := BuildKey("gcc", "release", opts, "")
	if k1 == k2 {
		t.Error("BuildKey should differ when mode differs")
	}
}

func TestBuildKeyDiffersOnOptions(t *testing.T) {
	k1 := BuildKey("gcc", "debug", map[string]any{"a": 1}, "")
	k2 := BuildKey("gcc", "debug", map[string]any{"a": 2}, "")
	if k1 == k2 {
		t.Error("BuildKey should differ when option value differs")
	}
}

func TestBuildKeyOrderIndependent(t *testing.T) {
	k1 := BuildKey("gcc", "debug", map[string]any{"a": 1, "b": 2}, "")
	k2 := BuildKey("gcc", "debug", map[string]any{"b": 2, "a": 1}, "")
	if k1 != k2 {
		t.Errorf("BuildKey should be order-independent for options: %q vs %q", k1, k2)
	}
}

func TestBuildKeyEmpty(t *testing.T) {
	k := BuildKey("", "", nil, "")
	if k == "" {
		t.Error("BuildKey should produce non-empty hash for empty input")
	}
}

func TestBuildKeyDiffersOnExtra(t *testing.T) {
	opts := map[string]any{"a": 1}
	k1 := BuildKey("gcc", "debug", opts, JoinKeyExtra("1.0.0", "abc", "h1", "", "s1"))
	k2 := BuildKey("gcc", "debug", opts, JoinKeyExtra("1.1.0", "abc", "h1", "", "s1"))
	k3 := BuildKey("gcc", "debug", opts, JoinKeyExtra("1.0.0", "def", "h1", "", "s1"))
	k4 := BuildKey("gcc", "debug", opts, JoinKeyExtra("1.0.0", "abc", "h2", "", "s1"))
	k5 := BuildKey("gcc", "debug", opts, JoinKeyExtra("1.0.0", "abc", "h1", "p1", "s1"))
	k6 := BuildKey("gcc", "debug", opts, JoinKeyExtra("1.0.0", "abc", "h1", "", "s2"))
	if k1 == k2 || k1 == k3 || k1 == k4 || k1 == k5 || k1 == k6 {
		t.Error("BuildKey should differ when version/commit/globalFlags/patchHash/scriptHash differ")
	}
	if BuildKey("gcc", "debug", opts, "") == k1 {
		t.Error("BuildKey should differ between empty and non-empty extra")
	}
}

func TestNewSchedulerDefaultsModeBeforeBuildingKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git-based tool probe")
	}
	tool, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	tc := &toolchain.Toolchain{Name: "test", Tools: toolchain.Tools{CC: tool, CXX: tool, AR: tool}}
	graph, err := NewBuildGraph(map[string]map[string]*api.Target{
		"p": {"p": makeTargetWithDeps("p").SetKind(api.TargetVoid)},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]*api.PkgDirs{"p": {SourceDir: filepath.Join(t.TempDir(), "p"), BuildDir: filepath.Join(t.TempDir(), "p-build")}}
	s, err := NewScheduler(graph, tc, dirs, "", nil, api.Platform{OS: runtime.GOOS})
	if err != nil {
		t.Fatal(err)
	}
	info, ok := s.GetPkgInfo("p")
	if !ok {
		t.Fatal("missing package info")
	}
	want := BuildKey(s.toolIdentity(), api.ModeDebug, nil, "")
	if info.BuildKey != want {
		t.Fatalf("BuildKey = %q, want debug key %q", info.BuildKey, want)
	}
}

func TestSchedulerUsesPackageLayoutKeys(t *testing.T) {
	layouts := map[string]*PackageLayout{
		"p": {
			Dirs:     api.PkgDirs{SourceDir: "/src/p", BuildDir: "/stable/output"},
			BuildKey: "revision-and-flags",
			Ready:    true,
		},
		"q": {
			Dirs:     api.PkgDirs{SourceDir: "/src/q", BuildDir: "/stable/q"},
			BuildKey: "q-key",
			Ready:    true,
		},
	}
	s := newScheduler(schedulerConfig{
		Toolchain: &toolchain.Toolchain{Name: "test"},
		Layouts:   layouts,
		Mode:      "debug",
		Tools:     &ResolvedTools{CC: "/tool/cc", identity: "tool-content"},
	})
	for name, layout := range layouts {
		info, ok := s.GetPkgInfo(name)
		if !ok {
			t.Fatalf("missing package info for %s", name)
		}
		if info.BuildKey != layout.BuildKey {
			t.Fatalf("%s key = %s, want %s", name, info.BuildKey, layout.BuildKey)
		}
		if info.BuildDir != layout.Dirs.BuildDir {
			t.Fatalf("%s output dir = %s, want %s", name, info.BuildDir, layout.Dirs.BuildDir)
		}
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
