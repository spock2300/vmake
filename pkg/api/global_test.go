package api

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

func TestMergeGlobalOptionsBuiltins(t *testing.T) {
	got, err := MergeGlobalOptions(nil, []string{"host", "arm-gcc"})
	if err != nil {
		t.Fatalf("MergeGlobalOptions: %v", err)
	}
	if got[ModeOptionName] == nil {
		t.Error("mode option missing")
	}
	if got[ToolchainOptionName] == nil {
		t.Error("toolchain option missing")
	}
	if got[ToolchainOptionName].Default() != "host" {
		t.Errorf("toolchain default = %v, want host (first in list)", got[ToolchainOptionName].Default())
	}
	if !reflect.DeepEqual(got[ToolchainOptionName].Values(), []string{"host", "arm-gcc"}) {
		t.Errorf("toolchain values = %v", got[ToolchainOptionName].Values())
	}
}

func TestMergeGlobalOptionsUserGlobal(t *testing.T) {
	userOpt := (&Option{}).
		SetType(OptionBool).
		SetDefault(true).
		SetGroup("Global")
	all := map[string]map[string]*Option{
		"pkg": {"user-flag": userOpt},
	}
	got, err := MergeGlobalOptions(all, nil)
	if err != nil {
		t.Fatalf("MergeGlobalOptions: %v", err)
	}
	if got["user-flag"] != userOpt {
		t.Error("user-defined global option should be included")
	}
}

func TestMergeGlobalOptionsConflictTypeMismatch(t *testing.T) {
	opt1 := (&Option{}).SetType(OptionBool).SetDefault(true).SetGroup("Global")
	opt2 := (&Option{}).SetType(OptionString).SetDefault("x").SetGroup("Global")
	all := map[string]map[string]*Option{
		"pkg1": {"g": opt1},
		"pkg2": {"g": opt2},
	}
	_, err := MergeGlobalOptions(all, nil)
	if err == nil {
		t.Error("type mismatch should produce error")
	}
}

func TestMergeGlobalOptionsConflictDefaultMismatch(t *testing.T) {
	opt1 := (&Option{}).SetType(OptionBool).SetDefault(true).SetGroup("Global")
	opt2 := (&Option{}).SetType(OptionBool).SetDefault(false).SetGroup("Global")
	all := map[string]map[string]*Option{
		"pkg1": {"g": opt1},
		"pkg2": {"g": opt2},
	}
	_, err := MergeGlobalOptions(all, nil)
	if err == nil {
		t.Error("default mismatch should produce error")
	}
}

func TestGetModeFlags(t *testing.T) {
	tests := []struct {
		mode        string
		wantCflags  []string
		wantDefines []string
	}{
		{ModeRelease, []string{"-O2"}, []string{"NDEBUG"}},
		{ModeDebug, []string{"-O0", "-g"}, nil},
		{"unknown", []string{"-O0", "-g"}, nil},
	}
	for _, tt := range tests {
		cflags, defines := GetModeFlags(tt.mode)
		if !reflect.DeepEqual(cflags, tt.wantCflags) {
			t.Errorf("GetModeFlags(%q) cflags = %v, want %v", tt.mode, cflags, tt.wantCflags)
		}
		if !reflect.DeepEqual(defines, tt.wantDefines) {
			t.Errorf("GetModeFlags(%q) defines = %v, want %v", tt.mode, defines, tt.wantDefines)
		}
	}
}

func TestAccessorSetOptions(t *testing.T) {
	a := NewConfigAccessor(nil, nil)
	opts := map[string]*Option{"x": {name: "x"}}
	a.SetOptions(opts)
	if a.Options["x"] == nil {
		t.Error("SetOptions did not replace")
	}
}

func TestConfigContextAllGlobalFlagsFuncs(t *testing.T) {
	var c, cxx, ld, links []string
	ctx := NewConfigContext("app").
		SetGlobalCFlagsFunc(func(f ...string) { c = append(c, f...) }).
		SetGlobalCxxFlagsFunc(func(f ...string) { cxx = append(cxx, f...) }).
		SetGlobalLdFlagsFunc(func(f ...string) { ld = append(ld, f...) }).
		SetGlobalLinksFunc(func(f ...string) { links = append(links, f...) })

	ctx.AddGlobalCFlags("-c")
	ctx.AddGlobalCxxFlags("-cxx")
	ctx.AddGlobalLdFlags("-ld")
	ctx.AddGlobalLinks("-lm")

	if !reflect.DeepEqual(c, []string{"-c"}) {
		t.Errorf("cflags = %v", c)
	}
	if !reflect.DeepEqual(cxx, []string{"-cxx"}) {
		t.Errorf("cxxflags = %v", cxx)
	}
	if !reflect.DeepEqual(ld, []string{"-ld"}) {
		t.Errorf("ldflags = %v", ld)
	}
	if !reflect.DeepEqual(links, []string{"-lm"}) {
		t.Errorf("links = %v", links)
	}
}

func TestConfigContextGetOptions(t *testing.T) {
	ctx := NewConfigContext("app")
	ctx.Option("foo").SetType(OptionBool)
	opts := ctx.GetOptions()
	if len(opts) != 1 || opts["foo"] == nil {
		t.Errorf("GetOptions = %v", opts)
	}
}

func TestBuildContextSetPackage(t *testing.T) {
	ctx := NewBuildContext("app", nil)
	pkg := NewPackage()
	ctx.SetPackage(pkg)
}

func TestCleanContextPackageDelegation(t *testing.T) {
	pkg := NewPackage()
	pkg.SetDirs(PkgDirs{SourceDir: "/src", BuildDir: "/build"})
	pkg.SetSrcDir("/src/actual")
	ctx := NewCleanContext("app", nil).SetPackage(pkg)

	if got := ctx.SourceDir(); got != "/src" {
		t.Errorf("SourceDir = %q", got)
	}
	if got := ctx.BuildDir(); got != "/build" {
		t.Errorf("BuildDir = %q", got)
	}
	if got := ctx.SrcDir(); got != "/src/actual" {
		t.Errorf("SrcDir = %q", got)
	}
}

func TestCopyFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dest := filepath.Join(dir, "dest.txt")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dest); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("dest content = %q", string(data))
	}
}

func TestCopyFileMissingSrc(t *testing.T) {
	err := CopyFile("/nonexistent/src", "/nonexistent/dest")
	if err == nil {
		t.Error("CopyFile should fail for missing source")
	}
}

func TestCopyDirBasic(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(src, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyDir(src, dest); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Errorf("a.txt missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "sub", "b.txt")); err != nil {
		t.Errorf("sub/b.txt missing: %v", err)
	}
}

func TestCopyDirSkipsGit(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".git", "config"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CopyDir(src, dest); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		t.Error(".git should be skipped")
	}
}

func TestCopyDirWithFilter(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "keep.c"), []byte("k"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skip.o"), []byte("s"), 0644); err != nil {
		t.Fatal(err)
	}
	filter := func(path string, isDir bool) bool {
		_, file := filepath.Split(path)
		_ = file
		ext := filepath.Ext(path)
		return ext != ".o"
	}
	if err := CopyDirWithFilter(src, dest, filter); err != nil {
		t.Fatalf("CopyDirWithFilter: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "keep.c")); err != nil {
		t.Errorf("keep.c missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "skip.o")); err == nil {
		t.Error("skip.o should be filtered out")
	}
}

func TestCopyDirIfExists(t *testing.T) {
	dest := t.TempDir()
	if err := CopyDirIfExists("/nonexistent_src_xyz", dest); err != nil {
		t.Errorf("CopyDirIfExists with missing src should be no-op: %v", err)
	}

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CopyDirIfExists(src, dest); err != nil {
		t.Fatalf("CopyDirIfExists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Errorf("a.txt missing: %v", err)
	}
}

func TestMatchPatterns(t *testing.T) {
	patterns := []string{"*.c", "*.h"}
	if !MatchPatterns(patterns, "foo.c") {
		t.Error("foo.c should match *.c")
	}
	if !MatchPatterns(patterns, "bar.h") {
		t.Error("bar.h should match *.h")
	}
	if MatchPatterns(patterns, "skip.o") {
		t.Error("skip.o should not match")
	}
}

func TestWriteConfigHeader(t *testing.T) {
	dir := t.TempDir()
	content := "#ifndef VMAKE_AUTOCONF_H\n#define VMAKE_AUTOCONF_H\n#define X 1\n#endif\n"
	if err := WriteConfigHeader(dir, content); err != nil {
		t.Fatalf("WriteConfigHeader: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "autoconf.h"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != content {
		t.Errorf("content = %q", string(data))
	}

	if err := WriteConfigHeader(dir, content); err != nil {
		t.Errorf("idempotent write should succeed: %v", err)
	}
}

func TestWriteConfigHeaderOverwriteOnChange(t *testing.T) {
	dir := t.TempDir()
	first := "#define A 1"
	if err := WriteConfigHeader(dir, first); err != nil {
		t.Fatal(err)
	}
	second := "#define B 2"
	if err := WriteConfigHeader(dir, second); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "autoconf.h"))
	if string(data) != second {
		t.Errorf("after change = %q, want %q", string(data), second)
	}
}

func TestApplyKConfigPatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".config")
	original := "CONFIG_FOO=n\nCONFIG_BAR=y\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	ApplyKConfigPatches(path, map[string]string{
		"CONFIG_FOO=n": "CONFIG_FOO=y",
	})
	data, _ := os.ReadFile(path)
	if string(data) != "CONFIG_FOO=y\nCONFIG_BAR=y\n" {
		t.Errorf("patched = %q", string(data))
	}
}

func TestApplyKConfigPatchesEmptyMapNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".config")
	original := "CONFIG_FOO=y\n"
	_ = os.WriteFile(path, []byte(original), 0644)

	ApplyKConfigPatches(path, nil)
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("empty patches should be no-op, got %q", string(data))
	}
}

func TestApplyKConfigPatchesMissingFile(t *testing.T) {
	ApplyKConfigPatches("/nonexistent/file", map[string]string{"a": "b"})
}

func TestEnsureConfigExistingValid(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".config")
	_ = os.WriteFile(configPath, []byte("CONFIG_X=y\n"), 0644)

	p := NewPackage()
	if p.EnsureConfig(dir) {
		t.Error("EnsureConfig should return false when valid .config exists")
	}
}

func TestEnsureConfigMissingTriggersMake(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("make may not be on PATH on Windows")
	}
	dir := t.TempDir()
	pkg := NewPackage()
	entry := pkg.AddKConfig("linux")
	entry.SetSelectedPreset("defconfig")

	makePath, err := findFakeMake(dir)
	if err != nil {
		t.Skip("could not create fake make:", err)
	}

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", filepath.Dir(makePath)+":"+origPath)
	defer os.Setenv("PATH", origPath)

	created := pkg.EnsureConfig(dir)
	if !created {
		t.Error("EnsureConfig should return true when it had to generate")
	}
	data, err := os.ReadFile(filepath.Join(dir, ".config"))
	if err != nil {
		t.Fatalf(".config not created: %v", err)
	}
	if len(data) == 0 {
		t.Error(".config should not be empty")
	}
}

func findFakeMake(dir string) (string, error) {
	makeDir := filepath.Join(dir, "fakebin")
	if err := os.Mkdir(makeDir, 0755); err != nil {
		return "", err
	}
	makePath := filepath.Join(makeDir, "make")
	script := []byte("#!/bin/sh\n[ \"$1\" = \"defconfig\" ] && echo \"CONFIG_FAKE=y\" > .config\n")
	if err := os.WriteFile(makePath, script, 0755); err != nil {
		return "", err
	}
	return makePath, nil
}

func TestPackageHomepageAndMetadata(t *testing.T) {
	p := NewPackage().
		SetHomepage("https://example.com").
		SetDescription("desc").
		SetLicense("MIT")
	if p.Homepage() != "https://example.com" {
		t.Errorf("Homepage = %q", p.Homepage())
	}
	if p.Description() != "desc" {
		t.Errorf("Description = %q", p.Description())
	}
	if p.License() != "MIT" {
		t.Errorf("License = %q", p.License())
	}
}

func TestPackageSubmodules(t *testing.T) {
	p := NewPackage()
	if p.Submodules() {
		t.Error("default should be false")
	}
	p.SetSubmodules(true)
	if !p.Submodules() {
		t.Error("should be true after SetSubmodules(true)")
	}
}

func TestPackageScriptDirAndOutputDir(t *testing.T) {
	p := NewPackage()
	p.SetScriptDir("/script")
	p.SetOutputDir("/output")
	if p.ScriptDir() != "/script" {
		t.Errorf("ScriptDir = %q", p.ScriptDir())
	}
	if p.OutputDir() != "/output" {
		t.Errorf("OutputDir = %q", p.OutputDir())
	}
}

func TestPackageConfigFiles(t *testing.T) {
	p := NewPackage()
	p.SetConfigFiles("a.cfg", "b.cfg")
	files := p.ConfigFiles()
	sort.Strings(files)
	if !reflect.DeepEqual(files, []string{"a.cfg", "b.cfg"}) {
		t.Errorf("ConfigFiles = %v", files)
	}
}

func TestPackageGetVersionsEmpty(t *testing.T) {
	p := NewPackage()
	if len(p.GetVersions()) != 0 {
		t.Errorf("GetVersions on empty = %v", p.GetVersions())
	}
	if _, err := p.SelectVersion(""); err == nil {
		t.Error("SelectVersion on no versions should error")
	}
}
