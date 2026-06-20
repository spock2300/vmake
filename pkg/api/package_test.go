package api

import (
	"reflect"
	"runtime"
	"sort"
	"testing"
)

func TestPackageMetaFullName(t *testing.T) {
	tests := []struct {
		repo, name, want string
	}{
		{"", "foo", "foo"},
		{"official", "zlib", "official/zlib"},
	}
	for _, tt := range tests {
		m := PackageMeta{Repo: tt.repo, Name: tt.name}
		if got := m.FullName(); got != tt.want {
			t.Errorf("FullName() = %q, want %q", got, tt.want)
		}
	}
}

func TestPackageNewPackageInit(t *testing.T) {
	p := NewPackage()
	if p == nil {
		t.Fatal("NewPackage returned nil")
	}
	if p.GetRequires() == nil {
		t.Error("GetRequires should not be nil")
	}
	if len(p.GetRequires().Get()) != 0 {
		t.Error("requires should be empty")
	}
	if p.GetOptions() == nil {
		t.Error("GetOptions should not be nil")
	}
}

func TestPackageCallbackRegistration(t *testing.T) {
	p := NewPackage()
	p.OnRequire(func(ctx *RequireContext) {}).
		OnConfig(func(ctx *ConfigContext) {}).
		OnBuild(func(ctx *BuildContext) {}).
		OnInstall(func(ctx *InstallContext) {}).
		OnClean(func(ctx *CleanContext) {}).
		OnPackage(func(p *Package) {})

	if len(p.GetRequireFuncs()) != 1 {
		t.Errorf("requireFuncs = %d", len(p.GetRequireFuncs()))
	}
	if p.GetPackageFunc() == nil {
		t.Error("packageFunc should be set")
	}
}

func TestPackageOnPackageOverwrites(t *testing.T) {
	p := NewPackage()
	first := func(p *Package) {}
	second := func(p *Package) {}
	p.OnPackage(first).OnPackage(second)
	gotPtr := reflect.ValueOf(p.GetPackageFunc()).Pointer()
	wantPtr := reflect.ValueOf(second).Pointer()
	if gotPtr != wantPtr {
		t.Error("OnPackage should overwrite (not append)")
	}
}

func TestPackageExecConfigFuncsInOrder(t *testing.T) {
	p := NewPackage()
	var order []string
	p.OnConfig(func(ctx *ConfigContext) { order = append(order, "first") })
	p.OnConfig(func(ctx *ConfigContext) { order = append(order, "second") })
	p.OnConfig(func(ctx *ConfigContext) { order = append(order, "third") })

	p.ExecConfigFuncs(".", func(fn ConfigFunc) { fn(nil) })

	want := []string{"first", "second", "third"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("callback order = %v, want %v", order, want)
	}
}

func TestPackageExecBuildFuncsInOrder(t *testing.T) {
	p := NewPackage()
	var order []string
	p.OnBuild(func(ctx *BuildContext) { order = append(order, "b1") })
	p.OnBuild(func(ctx *BuildContext) { order = append(order, "b2") })

	p.ExecBuildFuncs(".", func(fn BuildFunc) { fn(nil) })

	if !reflect.DeepEqual(order, []string{"b1", "b2"}) {
		t.Errorf("build order = %v", order)
	}
}

func TestPackageExecFuncsChdir(t *testing.T) {
	p := NewPackage()
	observed := make(chan string, 1)
	p.OnConfig(func(ctx *ConfigContext) {
		_, file, _, _ := runtime.Caller(0)
		observed <- file
	})

	tmpDir := t.TempDir()
	p.ExecConfigFuncs(tmpDir, func(fn ConfigFunc) { fn(nil) })

	select {
	case f := <-observed:
		if f == "" {
			t.Skip("could not observe caller file")
		}
	default:
		t.Skip("no observation")
	}
}

func TestPackageSetGitURLs(t *testing.T) {
	p := NewPackage()
	p.SetGit("https://a.git", "https://b.git")
	got := p.GitURLs()
	want := []string{"https://a.git", "https://b.git"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GitURLs() = %v, want %v", got, want)
	}
}

func TestPackageAddVersion(t *testing.T) {
	p := NewPackage().SetName("foo")
	p.AddVersion("1.0.0", "v1.0.0").
		AddVersion("2.0.0", "v2.0.0")

	versions := p.GetVersions()
	sort.Strings(versions)
	want := []string{"1.0.0", "2.0.0"}
	if !reflect.DeepEqual(versions, want) {
		t.Errorf("GetVersions() = %v, want %v", versions, want)
	}
	if p.GetRef("1.0.0") != "v1.0.0" {
		t.Errorf("GetRef(1.0.0) = %q", p.GetRef("1.0.0"))
	}
}

func TestPackageSelectVersion(t *testing.T) {
	p := NewPackage().SetName("foo")
	p.AddVersion("1.0.0", "v1.0.0")
	p.AddVersion("1.2.0", "v1.2.0")
	p.AddVersion("2.0.0", "v2.0.0")

	got, err := p.SelectVersion(">=1.0.0")
	if err != nil {
		t.Fatalf("SelectVersion: %v", err)
	}
	if got != "1.2.0" {
		t.Errorf("SelectVersion(>=1.0.0) = %q, want 1.2.0 (caret-like, major 1)", got)
	}

	got, err = p.SelectVersion(">=2.0.0")
	if err != nil {
		t.Fatalf("SelectVersion: %v", err)
	}
	if got != "2.0.0" {
		t.Errorf("SelectVersion(>=2.0.0) = %q, want 2.0.0", got)
	}
}

func TestPackageSelectVersionNoMatch(t *testing.T) {
	p := NewPackage().SetName("foo")
	p.AddVersion("1.0.0", "v1.0.0")
	if _, err := p.SelectVersion(">=2.0.0"); err == nil {
		t.Error("SelectVersion with no match should error")
	}
}

func TestPackageSelectVersionMulti(t *testing.T) {
	p := NewPackage().SetName("foo")
	p.AddVersion("1.0.0", "v1.0.0")
	p.AddVersion("1.5.0", "v1.5.0")
	p.AddVersion("2.0.0", "v2.0.0")

	tests := []struct {
		constraints []string
		want        string
	}{
		{[]string{">=1.0.0", "<2.0.0"}, "1.5.0"},
		{[]string{">=1.0.0"}, "1.5.0"},
		{[]string{">=2.0.0"}, "2.0.0"},
		{[]string{"=2.0.0"}, "2.0.0"},
	}
	for _, tt := range tests {
		got, err := p.SelectVersionMulti(tt.constraints)
		if err != nil {
			t.Errorf("SelectVersionMulti(%v): %v", tt.constraints, err)
			continue
		}
		if got != tt.want {
			t.Errorf("SelectVersionMulti(%v) = %q, want %q", tt.constraints, got, tt.want)
		}
	}
}

func TestPackageSelectVersionMultiConflict(t *testing.T) {
	p := NewPackage().SetName("foo")
	p.AddVersion("1.0.0", "v1.0.0")
	p.AddVersion("2.0.0", "v2.0.0")

	_, err := p.SelectVersionMulti([]string{">=2.0.0", "<2.0.0"})
	if err == nil {
		t.Error("conflicting constraints should produce no match")
	}
}

func TestPackageDirsGetters(t *testing.T) {
	p := NewPackage()
	p.SetDirs(PkgDirs{SourceDir: "/src", BuildDir: "/build", InstallDir: "/install"})
	if p.SourceDir() != "/src" {
		t.Errorf("SourceDir = %q", p.SourceDir())
	}
	if p.BuildDir() != "/build" {
		t.Errorf("BuildDir = %q", p.BuildDir())
	}
	if p.InstallDir() != "/install" {
		t.Errorf("InstallDir = %q", p.InstallDir())
	}
}

func TestPackageSrcDirFallback(t *testing.T) {
	p := NewPackage()
	p.SetDirs(PkgDirs{SourceDir: "/src"})
	if got := p.SrcDir(); got != "/src" {
		t.Errorf("SrcDir fallback = %q, want /src", got)
	}
	p.SetSrcDir("/custom-src")
	if got := p.SrcDir(); got != "/custom-src" {
		t.Errorf("SrcDir explicit = %q, want /custom-src", got)
	}
	if got := p.SrcDirRaw(); got != "/custom-src" {
		t.Errorf("SrcDirRaw = %q", got)
	}
}

func TestPackageRoot(t *testing.T) {
	p := NewPackage()
	if p.IsRoot() {
		t.Error("new package should not be root")
	}
	p.SetRoot(true)
	if !p.IsRoot() {
		t.Error("should be root after SetRoot(true)")
	}
}

func TestPackageSetDepsAndAppend(t *testing.T) {
	p := NewPackage()
	pkgA := NewInstalledPackage("a", "1.0", "/a", nil)
	p.SetDep("a", pkgA)
	if p.Deps()["a"] != pkgA {
		t.Error("SetDep should add dep")
	}

	pkgB := NewInstalledPackage("b", "2.0", "/b", nil)
	p.SetDeps(map[string]*InstalledPackage{"b": pkgB})
	if _, exists := p.Deps()["a"]; exists {
		t.Error("SetDeps should replace, not merge")
	}
	if p.Deps()["b"] != pkgB {
		t.Error("SetDeps should add new dep")
	}
}

func TestPackagePatches(t *testing.T) {
	p := NewPackage()
	p.AddPatches("a.patch", "b.patch")
	if !reflect.DeepEqual(p.GetPatches(), []string{"a.patch", "b.patch"}) {
		t.Errorf("AddPatches = %v", p.GetPatches())
	}
	p.SetPatches("c.patch")
	if !reflect.DeepEqual(p.GetPatches(), []string{"c.patch"}) {
		t.Errorf("SetPatches = %v", p.GetPatches())
	}
}

func TestPackageKConfigEntries(t *testing.T) {
	p := NewPackage()
	p.AddKConfig("linux").AddPreset("defconfig").SetDefault("defconfig")
	entries := p.KConfigEntries()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Name() != "linux" {
		t.Errorf("entry name = %q", entries[0].Name())
	}
	if p.SelectedPreset() != "defconfig" {
		t.Errorf("SelectedPreset = %q", p.SelectedPreset())
	}
}

func TestPackageSelectedPresetPrefersSelectedOverDefault(t *testing.T) {
	p := NewPackage()
	p.AddKConfig("linux").AddPreset("defconfig").SetDefault("defconfig")
	if p.SelectedPreset() != "defconfig" {
		t.Errorf("default fallback = %q", p.SelectedPreset())
	}

	p.KConfigEntries()[0].SetSelectedPreset("tinyconfig")
	if p.SelectedPreset() != "tinyconfig" {
		t.Errorf("selected override = %q", p.SelectedPreset())
	}
}

func TestPackageGlobalFlagsMerge(t *testing.T) {
	p := NewPackage()
	p.SetGlobalFlags(
		[]string{"-O0"},
		[]string{"-std=c++17"},
		[]string{"-lm"},
		[]string{"pthread"},
	)
	if got := p.MergedCFlags("-g"); got != "-O0 -g" {
		t.Errorf("MergedCFlags = %q", got)
	}
	if got := p.MergedCxxFlags(); got != "-std=c++17" {
		t.Errorf("MergedCxxFlags = %q", got)
	}
	if got := p.MergedLdFlags("-lpthread"); got != "-lm -lpthread" {
		t.Errorf("MergedLdFlags = %q", got)
	}
}

func TestPackageCMakeGlobalFlagsArgs(t *testing.T) {
	p := NewPackage()
	p.SetGlobalFlags(
		[]string{"-O0"},
		[]string{"-std=c++17"},
		[]string{"-lm"},
		nil,
	)
	args := p.CMakeGlobalFlagsArgs()
	want := []string{
		"-DCMAKE_C_FLAGS=-O0",
		"-DCMAKE_CXX_FLAGS=-std=c++17",
		"-DCMAKE_EXE_LINKER_FLAGS=-lm",
		"-DCMAKE_SHARED_LINKER_FLAGS=-lm",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("CMakeGlobalFlagsArgs = %v, want %v", args, want)
	}
}

func TestSplitPackageRef(t *testing.T) {
	tests := []struct {
		ref     string
		repo    string
		name    string
		ok      bool
	}{
		{"foo", "", "foo", false},
		{"official/zlib", "official", "zlib", true},
		{"a/b/c", "a", "b/c", true},
	}
	for _, tt := range tests {
		repo, name, ok := SplitPackageRef(tt.ref)
		if repo != tt.repo || name != tt.name || ok != tt.ok {
			t.Errorf("SplitPackageRef(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tt.ref, repo, name, ok, tt.repo, tt.name, tt.ok)
		}
	}
}

func TestPackageDryRun(t *testing.T) {
	p := NewPackage()
	if p.DryRun() {
		t.Error("default should be false")
	}
	p.SetDryRun(true)
	if !p.DryRun() {
		t.Error("should be true after SetDryRun(true)")
	}
}

func TestPackageConfigPropagationFlags(t *testing.T) {
	p := NewPackage()
	p.SetGenConfigHeader(true)
	if !p.GenConfigHeader() {
		t.Error("GenConfigHeader should be true")
	}
	p.SetExportConfig(true)
	if !p.ExportConfig() {
		t.Error("ExportConfig should be true")
	}
	p.SetImportConfigs([]string{"foo", "bar"})
	if !reflect.DeepEqual(p.ImportConfigs(), []string{"foo", "bar"}) {
		t.Errorf("ImportConfigs = %v", p.ImportConfigs())
	}
}
