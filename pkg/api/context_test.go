package api

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildContextConfigPropagationFlags(t *testing.T) {
	ctx := NewBuildContext("app", nil)

	ctx.GenerateConfigHeader()
	if !ctx.GenConfigHeader() {
		t.Error("GenConfigHeader should be true")
	}

	ctx.GenerateConfigDefines()
	if !ctx.GenConfigDefines() {
		t.Error("GenConfigDefines should be true")
	}

	ctx.ExportConfig()
	if !ctx.ExportEnabled() {
		t.Error("ExportEnabled should be true")
	}

	ctx.ImportConfig("foo", "bar")
	if !reflect.DeepEqual(ctx.ImportConfigs(), []string{"foo", "bar"}) {
		t.Errorf("ImportConfigs = %v", ctx.ImportConfigs())
	}
}

func TestBuildContextImportConfigAppends(t *testing.T) {
	ctx := NewBuildContext("app", nil)
	ctx.ImportConfig("a").ImportConfig("b").ImportConfig("c")
	if !reflect.DeepEqual(ctx.ImportConfigs(), []string{"a", "b", "c"}) {
		t.Errorf("ImportConfig not appending: %v", ctx.ImportConfigs())
	}
}

func TestBuildContextSyncConfigDefinesCombined(t *testing.T) {
	ctx := NewBuildContext("app", nil)
	ctx.SyncConfigDefines("dep1", "dep2")

	if !ctx.GenConfigDefines() {
		t.Error("SyncConfigDefines should enable GenConfigDefines")
	}
	if !reflect.DeepEqual(ctx.ImportConfigs(), []string{"dep1", "dep2"}) {
		t.Errorf("ImportConfigs = %v, want [dep1 dep2]", ctx.ImportConfigs())
	}
}

func TestBuildContextBuildSubGraphDryRun(t *testing.T) {
	ctx := NewBuildContext("app", nil).SetDryRun(true)
	called := false
	ctx.SetBuildSubGraphFunc(func(name string) error {
		called = true
		return nil
	})
	ctx.BuildSubGraph("codegen")
	if called {
		t.Error("BuildSubGraph should be skipped in dry-run")
	}
}

func TestBuildContextDepOutputDryRun(t *testing.T) {
	ctx := NewBuildContext("app", nil).SetDryRun(true)
	ctx.SetDepOutputFunc(func(ref string) string {
		return "should-not-call"
	})
	if got := ctx.DepOutput("any:thing"); got != "" {
		t.Errorf("DepOutput dry-run = %q, want empty", got)
	}
}

func TestBuildContextDepBuildDir(t *testing.T) {
	ctx := NewBuildContext("app", nil)
	ctx.SetDepOutputFunc(func(ref string) string {
		return "/build/dir/libfoo.a"
	})
	got := ctx.DepBuildDir("foo:bar")
	if want := filepath.FromSlash("/build/dir"); got != want {
		t.Errorf("DepBuildDir = %q, want %q", got, want)
	}
}

func TestConfigContextGlobalFlagsBuffered(t *testing.T) {
	var collected []string
	ctx := NewConfigContext("app")
	ctx.SetGlobalCFlagsFunc(func(flags ...string) {
		collected = append(collected, flags...)
	})

	ctx.AddGlobalCFlags("-a", "-b")
	if !reflect.DeepEqual(collected, []string{"-a", "-b"}) {
		t.Errorf("collected = %v", collected)
	}
}

func TestConfigContextSetDefaultVisibilityHidden(t *testing.T) {
	var cflags, cxxflags []string
	pkg := NewPackage()
	dep := NewPackage()
	ctx := NewConfigContextWithPackage("app", pkg)
	ctx.SetGlobalCFlagsFunc(func(f ...string) { cflags = append(cflags, f...) })
	ctx.SetGlobalCxxFlagsFunc(func(f ...string) { cxxflags = append(cxxflags, f...) })

	if ctx.SetDefaultVisibilityHidden().SetDefaultVisibilityHidden() != ctx {
		t.Fatal("visibility setter did not return the context")
	}
	if !pkg.DefaultVisibilityHidden() || dep.DefaultVisibilityHidden() {
		t.Fatal("visibility must be enabled only on the declaring package")
	}
	if len(cflags) != 0 || len(cxxflags) != 0 {
		t.Fatalf("package visibility leaked into global flags: C=%v CXX=%v", cflags, cxxflags)
	}
	c, cxx := pkg.VisibilityFlags()
	if !reflect.DeepEqual(c, []string{"-fvisibility=hidden"}) {
		t.Errorf("cflags = %v", c)
	}
	want := []string{"-fvisibility=hidden", "-fvisibility-inlines-hidden"}
	if !reflect.DeepEqual(cxx, want) {
		t.Errorf("cxxflags = %v, want %v", cxx, want)
	}
	if c, cxx := dep.VisibilityFlags(); len(c) != 0 || len(cxx) != 0 {
		t.Fatalf("undeclared dependency has visibility flags: %v %v", c, cxx)
	}
	c[0] = "changed"
	cxx[0] = "changed"
	c, cxx = pkg.VisibilityFlags()
	if c[0] != "-fvisibility=hidden" || cxx[0] != "-fvisibility=hidden" {
		t.Fatal("visibility getter exposes mutable package state")
	}
}

func TestConfigContextVisibilityWithoutPackageReportsScriptError(t *testing.T) {
	defer func() {
		err, ok := recover().(*BuildScriptError)
		if !ok || err.Package != "app" || err.Op != "SetDefaultVisibilityHidden" {
			t.Fatalf("expected package visibility script error, got %v", err)
		}
	}()
	NewConfigContext("app").SetDefaultVisibilityHidden()
	t.Fatal("expected script error")
}

func TestConfigContextKConfigDelegatesToPackage(t *testing.T) {
	pkg := NewPackage()
	ctx := NewConfigContextWithPackage("app", pkg)
	entry := ctx.KConfig("linux")
	if entry == nil {
		t.Fatal("KConfig returned nil")
	}
	if entry.Name() != "linux" {
		t.Errorf("name = %q", entry.Name())
	}
	if len(pkg.KConfigEntries()) != 1 {
		t.Errorf("pkg should have 1 kconfig entry, got %d", len(pkg.KConfigEntries()))
	}
}

func TestConfigContextSetProvidedLinkerScriptDelegates(t *testing.T) {
	pkg := NewPackage()
	ctx := NewConfigContextWithPackage("app", pkg)
	ctx.SetProvidedLinkerScript("linker.ld")
	if pkg.ProvidedLinkerScript() != "linker.ld" {
		t.Errorf("ProvidedLinkerScript = %q", pkg.ProvidedLinkerScript())
	}
}

func TestConfigContextSetConfigValue(t *testing.T) {
	ctx := NewConfigContext("app")
	ctx.SetConfigValue("foo", "bar")
	if got := ctx.String("foo"); got != "bar" {
		t.Errorf("String(foo) = %q", got)
	}
}

func TestConfigContextGlobalOption(t *testing.T) {
	ctx := NewConfigContext("app")
	opt := ctx.GlobalOption("my-global")
	if opt == nil {
		t.Fatal("GlobalOption returned nil")
	}
	if !opt.IsGlobal() {
		t.Error("option should be marked global")
	}
}

func TestConfigContextGlobalMode(t *testing.T) {
	ctx := NewConfigContext("app")
	opt := ctx.GlobalMode()
	if opt == nil {
		t.Fatal("GlobalMode returned nil")
	}
	if opt.Name() != ModeOptionName {
		t.Errorf("name = %q, want %q", opt.Name(), ModeOptionName)
	}
	if opt.Type() != OptionChoice {
		t.Errorf("type = %v, want OptionChoice", opt.Type())
	}
	if !opt.IsGlobal() {
		t.Error("mode option should be global")
	}
}

func TestConfigContextToolchainOption(t *testing.T) {
	ctx := NewConfigContext("app")
	opt := ctx.ToolchainOption()
	if opt == nil {
		t.Fatal("ToolchainOption returned nil")
	}
	if opt.Name() != ToolchainOptionName {
		t.Errorf("name = %q", opt.Name())
	}
	if opt.Default() != "host" {
		t.Errorf("default = %v, want host", opt.Default())
	}
}

func TestInstallContextPrefix(t *testing.T) {
	ctx := NewInstallContext("app", nil)
	if ctx.PrefixSet() {
		t.Error("PrefixSet should be false initially")
	}
	ctx.SetPrefix("/usr/local")
	if !ctx.PrefixSet() {
		t.Error("PrefixSet should be true after SetPrefix")
	}
	if got := ctx.Prefix(); got != "/usr/local" {
		t.Errorf("Prefix = %q", got)
	}
}

func TestInstallContextAddInstalls(t *testing.T) {
	ctx := NewInstallContext("app", nil)
	ctx.AddInstalls("src/bin", "bin/mybin")
	items := ctx.GetInstallItems()
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Src != "src/bin" || items[0].Dest != "bin/mybin" {
		t.Errorf("item = %+v", items[0])
	}
}

func TestNewBuildContextDefaults(t *testing.T) {
	ctx := NewBuildContext("app", map[string]any{"k": "v"})
	if ctx.PackageName() != "app" {
		t.Errorf("PackageName = %q", ctx.PackageName())
	}
	if ctx.String("k") != "v" {
		t.Errorf("String(k) = %q", ctx.String("k"))
	}
	if ctx.GetTargets() == nil {
		t.Error("GetTargets should not be nil")
	}
}

func TestNewCleanContextDefaults(t *testing.T) {
	ctx := NewCleanContext("app", map[string]any{"k": "v"})
	if ctx.PackageName() != "app" {
		t.Errorf("PackageName = %q", ctx.PackageName())
	}
	if ctx.String("k") != "v" {
		t.Errorf("String(k) = %q", ctx.String("k"))
	}
}
