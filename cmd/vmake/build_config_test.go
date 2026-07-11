package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
	needed := computeReachable(graph)
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
	needed := computeReachable(graph)
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
	needed := computeReachable(graph)
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
	needed := computeReachable(graph)
	if needed["orphan"] {
		t.Error("orphan (no one depends on it) should not be needed")
	}
	if !needed["app"] {
		t.Error("root should be needed")
	}
}

func TestCollectNeededLibraryOnlyStillPulledInViaBFS(t *testing.T) {
	// Per computeReachable logic (build_config.go:61-66), library-only packages
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
	needed := computeReachable(graph)
	if !needed["app"] {
		t.Error("consumer with requires should be needed")
	}
	if !needed["lib"] {
		t.Error("library-only package reachable via BFS should still be needed")
	}
}

func TestCollectNeededMultipleRootsFatals(t *testing.T) {
	t.Skip("vlog.Fatal calls os.Exit which kills the test process; covered by integration test in test_data/21_root_package")
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
			PatchKConfig(map[string]string{"CONFIG_OLD=n": "CONFIG_OLD=y"}),
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

func TestParseBuildGoExtractsVersionsAndGitURLs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "build.go")
	content := `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.AddVersion("1.0.0", "v1.0.0")
	p.AddVersion("2.0.0", "v2.0.0")
	p.SetGit("https://example.com/repo.git")
}
`
	_ = os.WriteFile(path, []byte(content), 0644)

	info, err := ParseBuildGo(path)
	if err != nil {
		t.Fatalf("ParseBuildGo: %v", err)
	}
	if len(info.Versions) != 2 {
		t.Errorf("Versions = %v, want 2 entries", info.Versions)
	}
	if info.Versions["1.0.0"] != "v1.0.0" {
		t.Errorf("Versions[1.0.0] = %q", info.Versions["1.0.0"])
	}
	if len(info.GitURLs) != 1 || info.GitURLs[0] != "https://example.com/repo.git" {
		t.Errorf("GitURLs = %v", info.GitURLs)
	}
}

func TestParseBuildGoMissingFile(t *testing.T) {
	_, err := ParseBuildGo("/nonexistent/build.go")
	if err == nil {
		t.Error("missing file should produce error")
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
		got := resolveMode(cfg, tt.flagVal)
		if got != tt.want {
			t.Errorf("resolveMode(flag=%q, config=%q) = %q, want %q",
				tt.flagVal, tt.configVal, got, tt.want)
		}
	}
}

func TestResolveToolchainNamePrecedence(t *testing.T) {
	cfg := &config.ConfigFile{Global: &config.GlobalConfig{Toolchain: "arm-gcc"}}
	got := resolveToolchainName(cfg, "explicit-tc")
	if got != "explicit-tc" {
		t.Errorf("flag should win: got %q", got)
	}

	got = resolveToolchainName(cfg, "")
	if got != "arm-gcc" {
		t.Errorf("config should be fallback: got %q", got)
	}

	cfg.Global.Toolchain = ""
	got = resolveToolchainName(cfg, "")
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
	dirs := makeLocalPkgDirs("/script", "/usr/bin/gcc", "debug", map[string]any{"x": 1})
	if dirs.SourceDir != "/script" {
		t.Errorf("SourceDir = %q", dirs.SourceDir)
	}
	if dirs.InstallDir != "" {
		t.Errorf("local pkgs should not have InstallDir, got %q", dirs.InstallDir)
	}
	if !stringsContains(dirs.BuildDir, "/script/build/") {
		t.Errorf("BuildDir should be under /script/build/: got %q", dirs.BuildDir)
	}
}

func TestMakeRemotePkgDirsLayout(t *testing.T) {
	dirs := makeRemotePkgDirs("/deps", "official/zlib", "/usr/bin/gcc", "release", map[string]any{"x": 1}, "/src")
	if dirs.SourceDir != "/src" {
		t.Errorf("SourceDir = %q", dirs.SourceDir)
	}
	if dirs.BuildDir != "/deps/official/zlib/out/"+filepath.Base(dirs.BuildDir)+"/build" {
	}
	if !stringsContains(dirs.BuildDir, "/deps/official/zlib/out/") {
		t.Errorf("BuildDir should be under deps/official/zlib/out/: got %q", dirs.BuildDir)
	}
	if !stringsContains(dirs.InstallDir, "/deps/official/zlib/out/") {
		t.Errorf("InstallDir should be under deps/official/zlib/out/: got %q", dirs.InstallDir)
	}
	if dirs.InstallDir == "" {
		t.Error("remote pkg should have InstallDir")
	}
}

func TestEnsureGitignoreIdempotent(t *testing.T) {
	dir := t.TempDir()
	gitignore := filepath.Join(dir, ".gitignore")

	ensureGitignore(dir)
	data1, _ := os.ReadFile(gitignore)

	ensureGitignore(dir)
	data2, _ := os.ReadFile(gitignore)

	if string(data1) != string(data2) {
		t.Errorf("ensureGitignore should not modify existing file")
	}
	if !stringsContains(string(data2), "vmake_deps") {
		t.Errorf(".gitignore should contain vmake_deps, got %q", string(data2))
	}
}

func TestEnsureGitignorePreservesExistingContent(t *testing.T) {
	dir := t.TempDir()
	gitignore := filepath.Join(dir, ".gitignore")
	original := "*.o\nbuild/\n"
	_ = os.WriteFile(gitignore, []byte(original), 0644)

	ensureGitignore(dir)
	data, _ := os.ReadFile(gitignore)
	if !stringsContains(string(data), "*.o") {
		t.Errorf("existing content should be preserved, got %q", string(data))
	}
	if !stringsContains(string(data), "vmake_deps") {
		t.Errorf("vmake_deps should be appended, got %q", string(data))
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

func TestCollectLocalPkgOptions(t *testing.T) {
	cfg := emptyConfig()
	config.SetEntry(cfg, "local", &config.EntryConfig{Options: map[string]any{"a": 1}})
	config.SetEntry(cfg, "remote", &config.EntryConfig{Options: map[string]any{"b": 2}})

	ctx := &RuntimeContext{Config: cfg}
	ctx.Resolver = resolver.NewResolver(nil, t.TempDir())
	ctx.Resolver.Graph().Order = []string{"local", "remote"}
	ctx.Resolver.Graph().Packages = map[string]*resolver.PackageNode{
		"local":  localNode("local"),
		"remote": remoteNode("remote"),
	}
	ctx.DepGraph = ctx.Resolver.Graph()

	got := collectLocalPkgOptions(ctx)
	if _, exists := got["local"]; !exists {
		t.Error("local package missing")
	}
	if _, exists := got["remote"]; exists {
		t.Error("remote package should be excluded from local-only collection")
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

var _ = sort.Strings
var _ = reflect.DeepEqual
