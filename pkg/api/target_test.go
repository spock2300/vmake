package api

import (
	"reflect"
	"testing"
)

func TestTargetKindExt(t *testing.T) {
	tests := []struct {
		kind TargetKind
		want string
	}{
		{TargetBinary, ""},
		{TargetStatic, ".a"},
		{TargetShared, ".so"},
		{TargetObject, ".o"},
		{TargetVoid, ""},
	}
	for _, tt := range tests {
		if got := tt.kind.Ext(); got != tt.want {
			t.Errorf("TargetKind(%q).Ext() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestTargetKindPrefix(t *testing.T) {
	tests := []struct {
		kind TargetKind
		want string
	}{
		{TargetBinary, ""},
		{TargetStatic, "lib"},
		{TargetShared, "lib"},
		{TargetObject, ""},
		{TargetVoid, ""},
	}
	for _, tt := range tests {
		if got := tt.kind.Prefix(); got != tt.want {
			t.Errorf("TargetKind(%q).Prefix() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestTargetKindInstallDir(t *testing.T) {
	tests := []struct {
		kind TargetKind
		want string
	}{
		{TargetBinary, "bin"},
		{TargetStatic, "lib"},
		{TargetShared, "lib"},
		{TargetObject, ""},
		{TargetVoid, ""},
	}
	for _, tt := range tests {
		if got := tt.kind.InstallDir(); got != tt.want {
			t.Errorf("TargetKind(%q).InstallDir() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestTargetDefaults(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("app")
	if tgt.Name() != "app" {
		t.Errorf("Name = %q, want app", tgt.Name())
	}
	if tgt.Kind() != TargetBinary {
		t.Errorf("Kind = %q, want binary", tgt.Kind())
	}
	if !tgt.IsDefault() {
		t.Error("IsDefault should be true for new target")
	}
	if tgt.IsTest() {
		t.Error("IsTest should be false for new target")
	}
}

func TestTargetRegistryIdempotent(t *testing.T) {
	tr := NewTargetRegistry()
	a := tr.Target("foo")
	b := tr.Target("foo")
	if a != b {
		t.Error("Target(name) should return same pointer for same name")
	}
	if len(tr.GetTargets()) != 1 {
		t.Errorf("got %d targets, want 1", len(tr.GetTargets()))
	}
}

func TestTargetSetKind(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("lib").SetKind(TargetStatic)
	if tgt.Kind() != TargetStatic {
		t.Errorf("Kind = %q, want static", tgt.Kind())
	}
}

func TestTargetSetTestClearsDefault(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t")
	if !tgt.IsDefault() {
		t.Fatal("default should be true initially")
	}
	tgt.SetTest(true)
	if tgt.IsTest() != true {
		t.Fatal("IsTest should be true after SetTest(true)")
	}
	if tgt.IsDefault() {
		t.Fatal("SetTest(true) must clear IsDefault")
	}
	tgt.SetTest(false)
	if tgt.IsDefault() {
		t.Error("SetTest(false) must NOT re-enable IsDefault (per source target.go:71-77)")
	}
	if tgt.IsTest() {
		t.Error("IsTest should be false after SetTest(false)")
	}
}

func TestTargetAddFiles(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t")

	tgt.AddFiles("a.c", "b.c")
	if !reflect.DeepEqual(tgt.Files(), []string{"a.c", "b.c"}) {
		t.Errorf("Files = %v, want [a.c b.c]", tgt.Files())
	}

	tgt.AddFiles([]string{"c.c", "d.c"})
	want := []string{"a.c", "b.c", "c.c", "d.c"}
	if !reflect.DeepEqual(tgt.Files(), want) {
		t.Errorf("Files = %v, want %v", tgt.Files(), want)
	}

	tgt.AddFiles("", "e.c", "")
	if !reflect.DeepEqual(tgt.Files(), append(want, "e.c")) {
		t.Errorf("empty strings should be filtered, got %v", tgt.Files())
	}
}

func TestTargetRemoveFiles(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddFiles("a.c", "b.c", "c.c")
	tgt.RemoveFiles("b.c")
	if !reflect.DeepEqual(tgt.Files(), []string{"a.c", "b.c", "c.c"}) {
		t.Errorf("Files still contains all entries: %v (actual filter happens at glob time)", tgt.Files())
	}
	if !reflect.DeepEqual(tgt.ExcludedFiles(), []string{"b.c"}) {
		t.Errorf("ExcludedFiles = %v, want [b.c]", tgt.ExcludedFiles())
	}
}

func TestTargetFlagsChains(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").
		AddCFlags("-Wall", "-O2").
		AddCxxFlags("-std=c++17").
		AddLdFlags("-lm")

	if !reflect.DeepEqual(tgt.CFlags(), []string{"-Wall", "-O2"}) {
		t.Errorf("CFlags = %v", tgt.CFlags())
	}
	if !reflect.DeepEqual(tgt.CxxFlags(), []string{"-std=c++17"}) {
		t.Errorf("CxxFlags = %v", tgt.CxxFlags())
	}
	if !reflect.DeepEqual(tgt.LdFlags(), []string{"-lm"}) {
		t.Errorf("LdFlags = %v", tgt.LdFlags())
	}
}

func TestTargetRemoveFlags(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").
		AddCFlags("-Wall", "-O2", "-g").
		AddDefines("A", "B", "C").
		AddLinks("m", "z", "pthread").
		AddDeps("x:a", "y:b", "z:c")

	tgt.RemoveCFlags("-g").
		RemoveDefines("B").
		RemoveLinks("z").
		RemoveDeps("y:b")

	if !reflect.DeepEqual(tgt.CFlags(), []string{"-Wall", "-O2"}) {
		t.Errorf("CFlags = %v", tgt.CFlags())
	}
	if !reflect.DeepEqual(tgt.Defines(), []string{"A", "C"}) {
		t.Errorf("Defines = %v", tgt.Defines())
	}
	if !reflect.DeepEqual(tgt.Links(), []string{"m", "pthread"}) {
		t.Errorf("Links = %v", tgt.Links())
	}
	if !reflect.DeepEqual(tgt.Deps(), []string{"x:a", "z:c"}) {
		t.Errorf("Deps = %v", tgt.Deps())
	}
}

func TestTargetAddDepsIgnoresEmpty(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddDeps("a:b", "", "c:d", "")
	if !reflect.DeepEqual(tgt.Deps(), []string{"a:b", "c:d"}) {
		t.Errorf("Deps = %v, want [a:b c:d]", tgt.Deps())
	}
}

func TestTargetHasDep(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddDeps("a:b", "c:d")
	if !tgt.HasDep("a:b") {
		t.Error("HasDep(a:b) should be true")
	}
	if tgt.HasDep("x:y") {
		t.Error("HasDep(x:y) should be false")
	}
}

func TestTargetAddPublicIncludesPlain(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddPublicIncludes("include", "third_party/foo/include")
	want := []string{"include", "third_party/foo/include"}
	if !reflect.DeepEqual(tgt.PublicIncludes(), want) {
		t.Errorf("PublicIncludes = %v, want %v", tgt.PublicIncludes(), want)
	}
}

func TestTargetAddPublicIncludesWithRule(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddPublicIncludes("include", "@*.h")
	if !reflect.DeepEqual(tgt.PublicIncludes(), []string{"include"}) {
		t.Errorf("PublicIncludes = %v, want [include]", tgt.PublicIncludes())
	}
	rules := tgt.IncludeRule("include")
	if !reflect.DeepEqual(rules, []string{"*.h"}) {
		t.Errorf("IncludeRule(include) = %v, want [*.h]", rules)
	}
}

func TestTargetAddPostLinkHelpers(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").
		AddPostLinkHex().
		AddPostLinkBin().
		AddPostLinkSize().
		AddPostLinkStrip()

	steps := tgt.PostLinkSteps()
	if len(steps) != 4 {
		t.Fatalf("got %d PostLinkSteps, want 4", len(steps))
	}
	if steps[0].Tool != "objcopy" {
		t.Errorf("step 0 tool = %q, want objcopy", steps[0].Tool)
	}
	if steps[1].Tool != "objcopy" {
		t.Errorf("step 1 tool = %q, want objcopy", steps[1].Tool)
	}
	if steps[2].Tool != "size" {
		t.Errorf("step 2 tool = %q, want size", steps[2].Tool)
	}
	if steps[3].Tool != "strip" {
		t.Errorf("step 3 tool = %q, want strip", steps[3].Tool)
	}
}

func TestTargetAddPostLinkDeps(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").
		AddPostLinkDeps("include/chip_public.sym")
	if !reflect.DeepEqual(tgt.PostLinkDeps(), []string{"include/chip_public.sym"}) {
		t.Fatalf("PostLinkDeps = %v, want [include/chip_public.sym]", tgt.PostLinkDeps())
	}
	tgt.AddPostLinkDeps("wrappers/hostap_public.sym", "include/lwip_public.sym")
	want := []string{"include/chip_public.sym", "wrappers/hostap_public.sym", "include/lwip_public.sym"}
	if !reflect.DeepEqual(tgt.PostLinkDeps(), want) {
		t.Errorf("PostLinkDeps = %v, want %v", tgt.PostLinkDeps(), want)
	}
}

func TestPostLinkStepOutputPaths(t *testing.T) {
	s := PostLinkStep{Tool: "objcopy", Args: []string{"-O", "ihex", "{output}", "{output}.hex"}}
	paths := s.OutputPaths("/build/app")
	if !reflect.DeepEqual(paths, []string{"/build/app.hex"}) {
		t.Errorf("OutputPaths = %v, want [/build/app.hex]", paths)
	}
}

func TestTargetSetSymbolBindingValidation(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t")
	tgt.SetSymbolBinding("static")
	if tgt.SymbolBinding() != "static" {
		t.Errorf("got %q", tgt.SymbolBinding())
	}
	tgt.SetSymbolBinding("static-functions")
	if tgt.SymbolBinding() != "static-functions" {
		t.Errorf("got %q", tgt.SymbolBinding())
	}
	tgt.SetSymbolBinding("")
	if tgt.SymbolBinding() != "" {
		t.Errorf("got %q", tgt.SymbolBinding())
	}
}
