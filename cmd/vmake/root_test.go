package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/pipeline"
)

func TestResolveInputsParams(t *testing.T) {
	cfg := &config.ConfigFile{}
	lock := &lockfile.Lock{}
	paths := &pipeline.Paths{
		ProjectDir: "/project",
		DepsDir:    "/project/.vmake_deps",
		CacheDir:   "/cache",
		ReposDir:   "/repos",
		LockPath:   "/project/.vmake/vmake.lock",
	}
	in := resolveInputs{
		ignoreLock:   true,
		workDir:      "/work",
		configPath:   "/project/.vmake/config.json",
		configDigest: "digest",
		lock:         lock,
		cfg:          cfg,
		paths:        paths,
	}
	got := in.params("release", "gcc")
	if got.WorkDir != "/work" || got.ConfigPath != "/project/.vmake/config.json" || got.ConfigDigest != "digest" {
		t.Fatalf("unexpected load fields: %+v", got)
	}
	if got.Config != cfg || got.Lock != lock || got.Paths != paths {
		t.Fatal("pointer fields were not preserved")
	}
	if got.LockPath != paths.LockPath {
		t.Errorf("LockPath = %q, want %q", got.LockPath, paths.LockPath)
	}
	if !got.IgnoreLock {
		t.Error("IgnoreLock = false, want true")
	}
	if got.ModeOverride != "release" || got.ToolchainOverride != "gcc" {
		t.Errorf("overrides = %q/%q", got.ModeOverride, got.ToolchainOverride)
	}
	if got.TrustChecker == nil {
		t.Error("TrustChecker should be set")
	}
}

func resetResolveTestGlobals(t *testing.T) {
	t.Helper()
	oldProjectDir := cachedProjectDir
	oldVmakeDir := vmakeDir
	oldOverride := projectOverride
	cachedProjectDir = ""
	vmakeDir = t.TempDir()
	projectOverride = ""
	t.Cleanup(func() {
		cachedProjectDir = oldProjectDir
		vmakeDir = oldVmakeDir
		projectOverride = oldOverride
	})
	t.Setenv("VMAKE_CACHE", t.TempDir())
}

func TestLoadResolveInputs(t *testing.T) {
	resetResolveTestGlobals(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".vmake"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	inputs, err := loadResolveInputs(true, false)
	if err != nil {
		t.Fatalf("loadResolveInputs: %v", err)
	}
	if inputs.workDir != dir {
		t.Errorf("workDir = %q, want %q", inputs.workDir, dir)
	}
	if inputs.cfg == nil || inputs.lock == nil || inputs.paths == nil {
		t.Fatal("loadResolveInputs returned incomplete inputs")
	}
	if !inputs.ignoreLock {
		t.Error("ignoreLock was not preserved")
	}
	if inputs.paths.ProjectDir != dir {
		t.Errorf("ProjectDir = %q, want %q", inputs.paths.ProjectDir, dir)
	}
	wantLock := filepath.Join(dir, ".vmake", lockfile.LockfileName)
	if inputs.paths.LockPath != wantLock {
		t.Errorf("LockPath = %q, want %q", inputs.paths.LockPath, wantLock)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		t.Errorf("ensureGitignore did not run: %v", err)
	}
}

func TestLoadResolveInputsSoftGitignore(t *testing.T) {
	resetResolveTestGlobals(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".vmake"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".gitignore"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if _, err := loadResolveInputs(false, false); err == nil {
		t.Fatal("strict gitignore failure should return an error")
	}
	if _, err := loadResolveInputs(false, true); err != nil {
		t.Fatalf("soft gitignore failure should continue: %v", err)
	}
}
