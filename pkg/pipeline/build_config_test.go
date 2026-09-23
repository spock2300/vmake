package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func localSource(name string) *buildscript.Source {
	return buildscript.NewSource(name, "/p/"+name+"/build.go", "/p/"+name, api.SourceLocal)
}

func localNode(name string, deps ...string) *resolver.PackageNode {
	return &resolver.PackageNode{
		ID:     name,
		Source: localSource(name),
		Pkg:    api.NewPackage(),
		Deps:   deps,
	}
}

func remoteNode(name string, deps ...string) *resolver.PackageNode {
	src := buildscript.NewSource(name, "/p/"+name+"/build.go", "/p/"+name, api.SourceRemote)
	return &resolver.PackageNode{
		ID:     name,
		Source: src,
		Pkg:    api.NewPackage(),
		Deps:   deps,
	}
}

func TestCollectNeededEmptyGraph(t *testing.T) {
	graph := &resolver.Graph{Packages: map[string]*resolver.PackageNode{}}
	needed, err := computeReachable(graph)
	if err != nil {
		t.Fatal(err)
	}
	if len(needed) != 0 {
		t.Errorf("empty graph should produce empty needed, got %v", needed)
	}
}

func TestCollectNeededSingleRootExplicit(t *testing.T) {
	root := localNode("app")
	root.Pkg.SetRoot(true)
	lib := localNode("lib")
	graph := &resolver.Graph{
		Packages: map[string]*resolver.PackageNode{
			"app": root,
			"lib": lib,
		},
	}
	needed, err := computeReachable(graph)
	if err != nil {
		t.Fatal(err)
	}
	if len(needed) != 1 {
		t.Fatalf("with explicit root, only root should be seed: got %v", needed)
	}
	if !needed["app"] {
		t.Error("app should be the seed")
	}
}

func TestCollectNeededBFSFromRoot(t *testing.T) {
	root := localNode("app", "lib")
	root.Pkg.SetRoot(true)
	lib := localNode("lib", "remote-pkg")
	remote := remoteNode("remote-pkg")
	graph := &resolver.Graph{
		Packages: map[string]*resolver.PackageNode{
			"app":        root,
			"lib":        lib,
			"remote-pkg": remote,
		},
	}
	needed, err := computeReachable(graph)
	if err != nil {
		t.Fatal(err)
	}
	if !needed["app"] || !needed["lib"] || !needed["remote-pkg"] {
		t.Errorf("BFS should reach all deps: got %v", needed)
	}
}

func TestCollectNeededExcludesUnreachable(t *testing.T) {
	root := localNode("app")
	root.Pkg.SetRoot(true)
	orphan := localNode("orphan")
	graph := &resolver.Graph{
		Packages: map[string]*resolver.PackageNode{
			"app":    root,
			"orphan": orphan,
		},
	}
	needed, err := computeReachable(graph)
	if err != nil {
		t.Fatal(err)
	}
	if needed["orphan"] {
		t.Error("orphan (no one depends on it) should not be needed")
	}
	if !needed["app"] {
		t.Error("root should be needed")
	}
}

func TestCollectNeededLibraryOnlyStillPulledInViaBFS(t *testing.T) {
	// Per computeReachable logic (keys.go), library-only packages
	// (no OnRequire + depended-on by local) are excluded from SEEDS, but they
	// are still pulled into the needed set via BFS from the consumer that
	// depends on them. The exclusion only affects which packages start the BFS.
	consumer := localNode("app", "lib")
	consumer.Pkg.OnRequire(func(ctx *api.RequireContext) {})
	libraryOnly := localNode("lib")
	graph := &resolver.Graph{
		Packages: map[string]*resolver.PackageNode{
			"app": consumer,
			"lib": libraryOnly,
		},
	}
	needed, err := computeReachable(graph)
	if err != nil {
		t.Fatal(err)
	}
	if !needed["app"] {
		t.Error("consumer with requires should be needed")
	}
	if !needed["lib"] {
		t.Error("library-only package reachable via BFS should still be needed")
	}
}

func TestCollectNeededMultipleRootsFails(t *testing.T) {
	r1 := localNode("app1")
	r1.Pkg.SetRoot(true)
	r2 := localNode("app2")
	r2.Pkg.SetRoot(true)
	graph := &resolver.Graph{
		Packages: map[string]*resolver.PackageNode{
			"app1": r1,
			"app2": r2,
		},
	}
	_, err := computeReachable(graph)
	if err == nil {
		t.Fatal("multiple SetRoot(true) packages should fail")
	}
	if !stringsContains(err.Error(), "multiple root packages") {
		t.Errorf("error should mention multiple root packages, got: %v", err)
	}
}

func emptyConfig() *config.ConfigFile {
	return &config.ConfigFile{
		Version: config.ConfigVersion,
		Global:  &config.GlobalConfig{Options: map[string]any{}},
		Entries: map[string]*config.EntryConfig{},
	}
}

func setupKConfigTest(t *testing.T, dir string) (*RuntimeContext, map[string]*api.PkgDirs, map[string]bool) {
	t.Helper()
	r := resolver.NewResolver(nil, t.TempDir())
	r.Graph().Order = []string{"p"}
	r.Graph().Packages["p"] = &resolver.PackageNode{ID: "p"}

	ctx := &RuntimeContext{
		Resolver:    r,
		Config:      emptyConfig(),
		AllKConfigs: map[string][]*api.KConfigEntry{},
	}
	pkgDirs := map[string]*api.PkgDirs{"p": {SourceDir: dir}}
	needed := map[string]bool{"p": true}
	return ctx, pkgDirs, needed
}

func TestRestoreKConfigFilesNoEntrySkips(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".config")
	_ = os.WriteFile(configPath, []byte("preexisting"), 0644)

	ctx, pkgDirs, needed := setupKConfigTest(t, dir)
	ctx.AllKConfigs["p"] = []*api.KConfigEntry{
		(&api.KConfigEntry{}).SetConfigPath(".config").SetSrcDir(dir),
	}

	if err := restoreKConfigFiles(ctx, pkgDirs, needed); err != nil {
		t.Fatalf("restoreKConfigFiles: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("preexisting .config should NOT be deleted when no config.json entry: %v", err)
	}
	if string(data) != "preexisting" {
		t.Errorf("file content = %q, want preexisting", string(data))
	}
}

func TestRestoreKConfigFilesEmptyKConfigDeletes(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".config")
	_ = os.WriteFile(configPath, []byte("preexisting"), 0644)

	ctx, pkgDirs, needed := setupKConfigTest(t, dir)
	ctx.AllKConfigs["p"] = []*api.KConfigEntry{
		(&api.KConfigEntry{}).SetConfigPath(".config").SetSrcDir(dir),
	}
	config.SetEntry(ctx.Config, "p", &config.EntryConfig{KConfig: ""})

	if err := restoreKConfigFiles(ctx, pkgDirs, needed); err != nil {
		t.Fatalf("restoreKConfigFiles: %v", err)
	}

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf(".config should be deleted when entry exists with empty kconfig: %v", err)
	}
}

func TestRestoreKConfigFilesWritesWhenChanged(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".config")

	ctx, pkgDirs, needed := setupKConfigTest(t, dir)
	ctx.AllKConfigs["p"] = []*api.KConfigEntry{
		(&api.KConfigEntry{}).SetConfigPath(".config").SetSrcDir(dir),
	}
	config.SetEntry(ctx.Config, "p", &config.EntryConfig{KConfig: "CONFIG_NEW=y\n"})

	if err := restoreKConfigFiles(ctx, pkgDirs, needed); err != nil {
		t.Fatalf("restoreKConfigFiles: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	if string(data) != "CONFIG_NEW=y\n" {
		t.Errorf("written content = %q, want CONFIG_NEW=y", string(data))
	}
}

func TestRestoreKConfigFilesSkipsWhenContentMatches(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".config")
	_ = os.WriteFile(configPath, []byte("CONFIG_SAME=y\n"), 0644)
	info, _ := os.Stat(configPath)
	origMtime := info.ModTime()

	ctx, pkgDirs, needed := setupKConfigTest(t, dir)
	ctx.AllKConfigs["p"] = []*api.KConfigEntry{
		(&api.KConfigEntry{}).SetConfigPath(".config").SetSrcDir(dir),
	}
	config.SetEntry(ctx.Config, "p", &config.EntryConfig{KConfig: "CONFIG_SAME=y\n"})

	if err := restoreKConfigFiles(ctx, pkgDirs, needed); err != nil {
		t.Fatal(err)
	}

	newInfo, _ := os.Stat(configPath)
	if !newInfo.ModTime().Equal(origMtime) {
		t.Errorf("file was rewritten; mtime should be preserved to avoid stamp invalidation")
	}
}

func TestRestoreKConfigFilesAppliesPatches(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".config")

	ctx, pkgDirs, needed := setupKConfigTest(t, dir)
	ctx.AllKConfigs["p"] = []*api.KConfigEntry{
		(&api.KConfigEntry{}).
			SetConfigPath(".config").
			SetSrcDir(dir).
			SetKConfigPatches(map[string]string{"CONFIG_OLD=n": "CONFIG_OLD=y"}),
	}
	config.SetEntry(ctx.Config, "p", &config.EntryConfig{KConfig: "CONFIG_OLD=n\n"})

	if err := restoreKConfigFiles(ctx, pkgDirs, needed); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(configPath)
	if string(data) != "CONFIG_OLD=y\n" {
		t.Errorf("patched content = %q, want CONFIG_OLD=y", string(data))
	}
}

func TestResolveModePrecedence(t *testing.T) {
	cfg := &config.ConfigFile{Global: &config.GlobalConfig{Mode: "release"}}
	tests := []struct {
		flagVal   string
		configVal string
		want      string
	}{
		{"", "", api.ModeDebug},
		{"release", "", "release"},
		{"debug", "release", "debug"},
	}
	for _, tt := range tests {
		cfg.Global.Mode = tt.configVal
		got := ResolveMode(cfg, tt.flagVal)
		if got != tt.want {
			t.Errorf("ResolveMode(flag=%q, config=%q) = %q, want %q",
				tt.flagVal, tt.configVal, got, tt.want)
		}
	}
}

func TestResolveToolchainNamePrecedence(t *testing.T) {
	cfg := &config.ConfigFile{Global: &config.GlobalConfig{Toolchain: "arm-gcc"}}
	got := ResolveToolchainName(cfg, "explicit-tc")
	if got != "explicit-tc" {
		t.Errorf("flag should win: got %q", got)
	}

	got = ResolveToolchainName(cfg, "")
	if got != "arm-gcc" {
		t.Errorf("config should be fallback: got %q", got)
	}

	cfg.Global.Toolchain = ""
	got = ResolveToolchainName(cfg, "")
	if got != "host" {
		t.Errorf("default should be host: got %q", got)
	}
}

func TestResolveWithDefault(t *testing.T) {
	tests := []struct {
		flag, cfgVal, def string
		want              string
	}{
		{"flag", "cfg", "def", "flag"},
		{"", "cfg", "def", "cfg"},
		{"", "", "def", "def"},
	}
	for _, tt := range tests {
		got := resolveWithDefault(tt.flag, tt.cfgVal, tt.def)
		if got != tt.want {
			t.Errorf("resolveWithDefault(%q,%q,%q) = %q, want %q",
				tt.flag, tt.cfgVal, tt.def, got, tt.want)
		}
	}
}

func TestMakeLocalPkgDirsLayout(t *testing.T) {
	dirs := makeLocalPkgDirs("/script", "/usr/bin/gcc@13", "debug", map[string]any{"x": 1}, "gh", "sh")
	if dirs.SourceDir != "/script" {
		t.Errorf("SourceDir = %q", dirs.SourceDir)
	}
	if dirs.InstallDir != "" {
		t.Errorf("local pkgs should not have InstallDir, got %q", dirs.InstallDir)
	}
	if !stringsContains(dirs.BuildDir, filepath.FromSlash("/script/build/")) {
		t.Errorf("BuildDir should be under /script/build/: got %q", dirs.BuildDir)
	}
}

func TestMakeRemotePkgDirsLayout(t *testing.T) {
	dirs := makeRemotePkgDirs("/vd", "/src", "/usr/bin/gcc@13", "release", map[string]any{"x": 1}, "1.0.0", "c0ffee", "gh", "", "sh")
	if dirs.SourceDir != filepath.Join(filepath.Dir(dirs.BuildDir), "work", "repo") {
		t.Errorf("SourceDir = %q", dirs.SourceDir)
	}
	if !stringsContains(dirs.BuildDir, filepath.FromSlash("/vd/out/")) {
		t.Errorf("BuildDir should be under /vd/out/: got %q", dirs.BuildDir)
	}
	if !stringsContains(dirs.InstallDir, filepath.FromSlash("/vd/out/")) {
		t.Errorf("InstallDir should be under /vd/out/: got %q", dirs.InstallDir)
	}
	if dirs.InstallDir == "" {
		t.Error("remote pkg should have InstallDir")
	}
}

func TestCollectAllPkgOptionsFiltersByNeeded(t *testing.T) {
	cfg := emptyConfig()
	config.SetEntry(cfg, "needed", &config.EntryConfig{Options: map[string]any{"x": 1}})
	config.SetEntry(cfg, "unneeded", &config.EntryConfig{Options: map[string]any{"y": 2}})

	ctx := &RuntimeContext{Config: cfg}
	ctx.Resolver = resolver.NewResolver(nil, t.TempDir())
	ctx.Resolver.Graph().Order = []string{"needed", "unneeded"}

	needed := map[string]bool{"needed": true}
	got := collectAllPkgOptions(ctx, needed)
	if _, exists := got["needed"]; !exists {
		t.Error("needed package missing")
	}
	if _, exists := got["unneeded"]; exists {
		t.Error("unneeded package should be filtered out")
	}
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && sortStringSearch(s, sub)
}

func sortStringSearch(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
