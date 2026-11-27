package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/pipeline"
)

func TestNewRuntimeContextPreservesParams(t *testing.T) {
	cfg := &config.ConfigFile{}
	lock := &lockfile.Lock{}
	paths := &pipeline.Paths{
		ProjectDir: "/project",
		DepsDir:    "/project/.vmake_deps",
		CacheDir:   "/cache",
		ReposDir:   "/repos",
		LockPath:   "/project/.vmake/vmake.lock",
	}
	params := pipeline.ResolveParams{
		WorkDir:           "/work",
		ConfigPath:        "/project/configs/default.json",
		ConfigDigest:      "digest",
		Config:            cfg,
		Lock:              lock,
		LockPath:          paths.LockPath,
		IgnoreLock:        true,
		Paths:             paths,
		ModeOverride:      "release",
		ToolchainOverride: "gcc",
		TrustChecker:      remoteScriptTrustChecker,
	}
	ctx := newRuntimeContext(params, nil)
	if ctx.WorkDir != "/work" || ctx.ConfigPath != "/project/configs/default.json" || ctx.ConfigDigest != "digest" {
		t.Fatalf("unexpected load fields: %+v", ctx)
	}
	if ctx.Config != cfg || ctx.Lock != lock || ctx.Paths != paths {
		t.Fatal("pointer fields were not preserved")
	}
	if ctx.LockPath != paths.LockPath {
		t.Errorf("LockPath = %q, want %q", ctx.LockPath, paths.LockPath)
	}
	if !ctx.LockPolicy.IgnoreAll {
		t.Error("LockPolicy.IgnoreAll = false, want true")
	}
	if ctx.ModeOverride != "release" || ctx.ToolchainOverride != "gcc" {
		t.Errorf("overrides = %q/%q", ctx.ModeOverride, ctx.ToolchainOverride)
	}
	if ctx.TrustChecker == nil {
		t.Error("TrustChecker should be set")
	}
}

func arrangeResolveTestGlobals(t *testing.T) {
	t.Helper()
	resetProcessState(runEnv{VmakeDir: t.TempDir()})
	projectOverride = ""
	configFlag = ""
	setFlags = nil
	unsetFlags = nil
	ephemeralSetFlags = nil
	t.Cleanup(func() { resetProcessState(runEnv{}) })
	t.Setenv("VMAKE_CACHE", t.TempDir())
}

func TestLoadResolveParams(t *testing.T) {
	arrangeResolveTestGlobals(t)
	dir := canonicalTempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, ".vmake"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	oldMode, oldToolchain := modeFlag, toolchainFlag
	modeFlag, toolchainFlag = "release", "gcc"
	t.Cleanup(func() {
		modeFlag, toolchainFlag = oldMode, oldToolchain
	})

	params, err := loadResolveParamsAt(findProjectDir(), true, false, false, "")
	if err != nil {
		t.Fatalf("loadResolveParams: %v", err)
	}
	if params.WorkDir != dir {
		t.Errorf("workDir = %q, want %q", params.WorkDir, dir)
	}
	if params.Config == nil || params.Lock == nil || params.Paths == nil {
		t.Fatal("loadResolveParams returned incomplete inputs")
	}
	if !params.IgnoreLock {
		t.Error("ignoreLock was not preserved")
	}
	if params.ModeOverride != "release" || params.ToolchainOverride != "gcc" {
		t.Errorf("overrides = %q/%q", params.ModeOverride, params.ToolchainOverride)
	}
	if params.TrustChecker == nil {
		t.Error("TrustChecker should be set")
	}
	if params.Paths.ProjectDir != dir {
		t.Errorf("ProjectDir = %q, want %q", params.Paths.ProjectDir, dir)
	}
	wantLock := filepath.Join(dir, ".vmake", lockfile.LockfileName)
	if params.Paths.LockPath != wantLock {
		t.Errorf("LockPath = %q, want %q", params.Paths.LockPath, wantLock)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		t.Errorf("ensureGitignore did not run: %v", err)
	}
}

func TestLoadResolveParamsSoftGitignore(t *testing.T) {
	arrangeResolveTestGlobals(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".vmake"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".gitignore"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if _, err := loadResolveParamsAt(findProjectDir(), false, false, false, ""); err == nil {
		t.Fatal("strict gitignore failure should return an error")
	}
	if _, err := loadResolveParamsAt(findProjectDir(), false, true, false, ""); err != nil {
		t.Fatalf("soft gitignore failure should continue: %v", err)
	}
}
