package api

import (
	"reflect"
	"testing"
)

func TestKConfigEntryFluent(t *testing.T) {
	k := (&KConfigEntry{name: "linux"}).
		SetDescription("Linux kernel").
		SetConfigPath(".config").
		SetSrcDir("/src/linux").
		SetMenuconfigCmd("menuconfig").
		AddPreset("defconfig").
		AddPreset("tinyconfig").
		SetDefault("defconfig").
		SetSelectedPreset("tinyconfig").
		PatchKConfig(map[string]string{"CONFIG_FOO=n": "CONFIG_FOO=y"})

	if k.Name() != "linux" {
		t.Errorf("Name = %q", k.Name())
	}
	if k.Description() != "Linux kernel" {
		t.Errorf("Description = %q", k.Description())
	}
	if k.ConfigPath() != ".config" {
		t.Errorf("ConfigPath = %q", k.ConfigPath())
	}
	if k.SrcDir() != "/src/linux" {
		t.Errorf("SrcDir = %q", k.SrcDir())
	}
	if k.MenuconfigCmd() != "menuconfig" {
		t.Errorf("MenuconfigCmd = %q", k.MenuconfigCmd())
	}
	if !reflect.DeepEqual(k.Presets(), []string{"defconfig", "tinyconfig"}) {
		t.Errorf("Presets = %v", k.Presets())
	}
	if k.DefaultPreset() != "defconfig" {
		t.Errorf("DefaultPreset = %q", k.DefaultPreset())
	}
	if k.SelectedPreset() != "tinyconfig" {
		t.Errorf("SelectedPreset = %q", k.SelectedPreset())
	}
	if !reflect.DeepEqual(k.Patches(), map[string]string{"CONFIG_FOO=n": "CONFIG_FOO=y"}) {
		t.Errorf("Patches = %v", k.Patches())
	}
}

func TestKConfigEntryEmptyDefaults(t *testing.T) {
	k := &KConfigEntry{name: "x"}
	if k.DefaultPreset() != "" {
		t.Errorf("DefaultPreset default = %q", k.DefaultPreset())
	}
	if k.SelectedPreset() != "" {
		t.Errorf("SelectedPreset default = %q", k.SelectedPreset())
	}
	if k.Presets() != nil {
		t.Errorf("Presets default = %v", k.Presets())
	}
	if k.Patches() != nil {
		t.Errorf("Patches default = %v", k.Patches())
	}
}

func TestGenRuleBinHeader(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddBinHeader("assets/logo.png")
	rules := tgt.GenRules()
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	r := rules[0]
	if r.Kind() != GenRuleBinHeader {
		t.Errorf("Kind = %q", r.Kind())
	}
	if r.Input() != "assets/logo.png" {
		t.Errorf("Input = %q", r.Input())
	}
	if r.OutputStem() != "logo" {
		t.Errorf("OutputStem = %q, want logo", r.OutputStem())
	}
}

func TestTargetInstallDir(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").SetInstallDir("/custom/install")
	if tgt.InstallDir() != "/custom/install" {
		t.Errorf("InstallDir = %q", tgt.InstallDir())
	}
}

func TestTargetSetInstall(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").SetInstall(false)
	if !tgt.NoInstall() {
		t.Error("NoInstall should be true after SetInstall(false)")
	}
	tgt.SetInstall(true)
	if tgt.NoInstall() {
		t.Error("NoInstall should be false after SetInstall(true)")
	}
}

func TestTargetExcludeLibs(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").
		SetExcludeLibs("libfoo", "libbar")

	if !reflect.DeepEqual(tgt.ExcludeLibs(), []string{"libfoo", "libbar"}) {
		t.Errorf("ExcludeLibs = %v", tgt.ExcludeLibs())
	}
}

func TestTargetSetBuildFunc(t *testing.T) {
	tr := NewTargetRegistry()
	fn := func(p *Package) error { return nil }
	tgt := tr.Target("t").SetBuildFunc(fn)
	if tgt.BuildFunc() == nil {
		t.Error("BuildFunc should not be nil after SetBuildFunc")
	}
}

func TestTargetAddProvidedLibs(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddProvidedLibs("libfoo.a", "libbar.a")
	if !reflect.DeepEqual(tgt.ProvidedLibs(), []string{"libfoo.a", "libbar.a"}) {
		t.Errorf("ProvidedLibs = %v", tgt.ProvidedLibs())
	}
}

func TestTargetSetLanguages(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").SetLanguages("c", "c++")
	if !reflect.DeepEqual(tgt.Languages(), []string{"c", "c++"}) {
		t.Errorf("Languages = %v", tgt.Languages())
	}
}

func TestTargetAddLinks(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddLinks("m", "pthread")
	if !reflect.DeepEqual(tgt.Links(), []string{"m", "pthread"}) {
		t.Errorf("Links = %v", tgt.Links())
	}
}

func TestTargetAddIncludes(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddIncludes("include", "/usr/include/foo")
	if !reflect.DeepEqual(tgt.Includes(), []string{"include", "/usr/include/foo"}) {
		t.Errorf("Includes = %v", tgt.Includes())
	}
}

func TestTargetAddDefines(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddDefines("FOO=1", "BAR")
	if !reflect.DeepEqual(tgt.Defines(), []string{"FOO=1", "BAR"}) {
		t.Errorf("Defines = %v", tgt.Defines())
	}
}

func TestTargetRemoveCxxFlags(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddCxxFlags("-std=c++17", "-O2", "-g")
	tgt.RemoveCxxFlags("-g")
	if !reflect.DeepEqual(tgt.CxxFlags(), []string{"-std=c++17", "-O2"}) {
		t.Errorf("CxxFlags = %v", tgt.CxxFlags())
	}
}

func TestTargetRemoveLdFlags(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddLdFlags("-lm", "-lpthread", "-latomic")
	tgt.RemoveLdFlags("-lpthread")
	if !reflect.DeepEqual(tgt.LdFlags(), []string{"-lm", "-latomic"}) {
		t.Errorf("LdFlags = %v", tgt.LdFlags())
	}
}

func TestTargetRemoveIncludesAndPublic(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").
		AddIncludes("a", "b", "c").
		AddPublicIncludes("x", "y", "z")
	tgt.RemoveIncludes("b").RemovePublicIncludes("y")
	if !reflect.DeepEqual(tgt.Includes(), []string{"a", "c"}) {
		t.Errorf("Includes = %v", tgt.Includes())
	}
	if !reflect.DeepEqual(tgt.PublicIncludes(), []string{"x", "z"}) {
		t.Errorf("PublicIncludes = %v", tgt.PublicIncludes())
	}
}

func TestTargetRemoveProvidedLibs(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddProvidedLibs("a", "b", "c")
	tgt.RemoveProvidedLibs("b")
	if !reflect.DeepEqual(tgt.ProvidedLibs(), []string{"a", "c"}) {
		t.Errorf("ProvidedLibs = %v", tgt.ProvidedLibs())
	}
}

func TestTargetRegistrySetDefaultFlags(t *testing.T) {
	tr := NewTargetRegistry()
	tr.SetDefaultFlags(
		[]string{"-DCFLAG"},
		[]string{"-DCXXFLAG"},
		[]string{"-DLDFLAG"},
	)
	tgt := tr.Target("t")
	if !reflect.DeepEqual(tgt.CFlags(), []string{"-DCFLAG"}) {
		t.Errorf("CFlags = %v", tgt.CFlags())
	}
	if !reflect.DeepEqual(tgt.CxxFlags(), []string{"-DCXXFLAG"}) {
		t.Errorf("CxxFlags = %v", tgt.CxxFlags())
	}
	if !reflect.DeepEqual(tgt.LdFlags(), []string{"-DLDFLAG"}) {
		t.Errorf("LdFlags = %v", tgt.LdFlags())
	}
}

func TestTargetUseDependencyLinkerScript(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t")
	if tgt.UseDepLinkerScript() {
		t.Error("UseDepLinkerScript should be false by default")
	}
	tgt.UseDependencyLinkerScript()
	if !tgt.UseDepLinkerScript() {
		t.Error("UseDepLinkerScript should be true after call")
	}
}

func TestTargetAddPostLinkCustom(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").AddPostLink("custom-tool", "--flag", "{output}")
	steps := tgt.PostLinkSteps()
	if len(steps) != 1 {
		t.Fatalf("got %d steps, want 1", len(steps))
	}
	if steps[0].Tool != "custom-tool" {
		t.Errorf("Tool = %q", steps[0].Tool)
	}
	if !reflect.DeepEqual(steps[0].Args, []string{"--flag", "{output}"}) {
		t.Errorf("Args = %v", steps[0].Args)
	}
}

func TestTargetPrebuilt(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").SetPrebuilt("/path/to/prebuilt.so")
	if tgt.Prebuilt() != "/path/to/prebuilt.so" {
		t.Errorf("Prebuilt = %q", tgt.Prebuilt())
	}
}

func TestTargetLinkerScript(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").SetLinkerScript("linker.ld")
	if tgt.LinkerScript() != "linker.ld" {
		t.Errorf("LinkerScript = %q", tgt.LinkerScript())
	}
}

func TestTargetVersionScript(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").SetVersionScript("export.map")
	if tgt.VersionScript() != "export.map" {
		t.Errorf("VersionScript = %q", tgt.VersionScript())
	}
}

func TestTargetSymbolPrefix(t *testing.T) {
	tr := NewTargetRegistry()
	tgt := tr.Target("t").SetSymbolPrefix("vmake_")
	if tgt.SymbolPrefix() != "vmake_" {
		t.Errorf("SymbolPrefix = %q", tgt.SymbolPrefix())
	}
	steps := tgt.PostLinkSteps()
	found := false
	for _, s := range steps {
		if s.Tool == "objcopy" {
			for _, a := range s.Args {
				if a == "--prefix-symbols=vmake_" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("SetSymbolPrefix should append objcopy step: %+v", steps)
	}
}

func TestInstallItemHolder(t *testing.T) {
	h := &InstallItemHolder{}
	h.AddInstalls("a", "b").AddInstalls("c", "d")
	items := h.GetInstallItems()
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Src != "a" || items[0].Dest != "b" {
		t.Errorf("item 0 = %+v", items[0])
	}

	filter := func(path string, isTargetOutput bool) bool { return true }
	h.SetInstallFilter(filter)
	if h.GetInstallFilter() == nil {
		t.Error("filter should be set")
	}
}

func TestNewInstalledPackageDefaults(t *testing.T) {
	ip := NewInstalledPackage("foo", "1.0", "/install", []string{"libfoo.a"})
	if ip.Name != "foo" {
		t.Errorf("Name = %q", ip.Name)
	}
	if ip.Version != "1.0" {
		t.Errorf("Version = %q", ip.Version)
	}
	if ip.IncludeDir != "/install/include" {
		t.Errorf("IncludeDir = %q", ip.IncludeDir)
	}
	if ip.BinDir != "/install/bin" {
		t.Errorf("BinDir = %q", ip.BinDir)
	}
	if !reflect.DeepEqual(ip.Libs, []string{"libfoo.a"}) {
		t.Errorf("Libs = %v", ip.Libs)
	}
}
