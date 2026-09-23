package pipeline

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitcmd"
	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func workspaceBindingFixture(t *testing.T, remote bool) (*buildPhaseState, *api.KConfigEntry, string) {
	t.Helper()
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	s := sessionFixture(t)
	s.ctx.DepGraph.Order = []string{"app"}
	s.needed = map[string]bool{"app": true}
	node := s.ctx.DepGraph.Packages["app"]
	versionDir := t.TempDir()
	seed := filepath.Join(versionDir, "src")
	if err := os.MkdirAll(filepath.Join(seed, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"build.go":       "package main\n",
		"nested/input.c": "int value = 1;\n",
		"nested/blocked": "regular file\n",
	} {
		if err := os.WriteFile(filepath.Join(seed, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", gitcmd.Args(args...)...)
		cmd.Dir = seed
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, output, err)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "test")
	git("config", "user.email", "test@example.com")
	git("add", ".")
	git("commit", "-q", "-m", "source")
	commit, err := repo.GetCurrentCommit(seed)
	if err != nil {
		t.Fatal(err)
	}
	if remote {
		node.Source = buildscript.NewSource("app", filepath.Join(seed, "build.go"), seed, api.SourceRemote)
	} else {
		node.Pkg.SetGit(seed)
	}
	node.Pkg.SetScriptDir(node.Source.Dir).AddPatches("change.patch")
	patch := "--- a/nested/input.c\n+++ b/nested/input.c\n@@ -1 +1 @@\n-int value = 1;\n+int value = 2;\n"
	if err := os.WriteFile(filepath.Join(node.Pkg.ScriptDir(), "change.patch"), []byte(patch), 0644); err != nil {
		t.Fatal(err)
	}
	tools, err := s.toolsForPackage("app")
	if err != nil {
		t.Fatal(err)
	}
	scriptHash, err := s.scriptHashFor("app")
	if err != nil {
		t.Fatal(err)
	}
	flagsHash := packageFlagsHash(s.globalFlagsHash, node)
	if remote {
		s.remote.entries["app"] = &config.EntryConfig{Version: "1.0.0"}
		s.remote.versionDirs["app"] = versionDir
		s.remote.commits["app"] = commit
		s.patchHashes["app"], err = repo.PatchSetHash(node.Pkg)
		if err != nil {
			t.Fatal(err)
		}
		s.pkgDirs["app"] = makeRemotePkgDirs(versionDir, seed, tools.CCKey(), s.cfg.Mode, s.allPkgOptions["app"], "1.0.0", commit, flagsHash, s.patchHashes["app"], scriptHash)
	} else {
		if err := s.selectPackageSource("app"); err != nil {
			t.Fatal(err)
		}
		s.pkgDirs["app"] = makeLocalPkgDirs(node.Source.Dir, tools.CCKey(), s.cfg.Mode, s.allPkgOptions["app"], flagsHash, scriptHash, s.sourceCommits["app"])
	}
	configRoot := filepath.Join(node.Source.Dir, "nested")
	if !remote {
		configRoot = filepath.Join(node.Source.Dir, "src", "nested")
	}
	k := (&api.KConfigEntry{}).SetSrcDir(configRoot).SetConfigPath(".config")
	s.ctx.AllKConfigs = map[string][]*api.KConfigEntry{"app": {k}}
	config.SetEntry(s.ctx.Config, "app", &config.EntryConfig{KConfig: "CONFIG_READY=y\n"})
	if err := s.preparePackageWorkspace("app"); err != nil {
		t.Fatal(err)
	}
	if err := s.applyPatchesToNeeded(); err != nil {
		t.Fatal(err)
	}
	if err := s.restoreKConfigs(); err != nil {
		t.Fatal(err)
	}
	return s, k, seed
}

func switchWorkspaceToolchain(t *testing.T, s *buildPhaseState) {
	t.Helper()
	tc := *s.cfg.Tc
	tc.Name = "workspace-" + storage.OwnerKey(s.ctx.Paths.ProjectDir)[:16]
	if err := toolchain.GetManager().RegisterToolchain(tc.Name, &tc); err != nil {
		t.Fatal(err)
	}
	s.scopeValues = maps.Clone(s.cfg.GlobalValues)
	s.scopeValues[api.ToolchainOptionName] = tc.Name
}

func TestSessionWorkspaceRebindRestoresPatchesAndKConfigBeforeOnBuild(t *testing.T) {
	for _, remote := range []bool{false, true} {
		name := "local-setgit"
		if remote {
			name = "remote"
		}
		t.Run(name, func(t *testing.T) {
			s, k, seed := workspaceBindingFixture(t, remote)
			node := s.ctx.DepGraph.Packages["app"]
			oldDirs := *s.pkgDirs["app"]
			oldConfig, err := filepath.EvalSymlinks(filepath.Join(k.SrcDir(), k.ConfigPath()))
			if err != nil {
				t.Fatal(err)
			}
			mtime := time.Unix(1700000000, 0)
			if err := os.Chtimes(oldConfig, mtime, mtime); err != nil {
				t.Fatal(err)
			}
			switchWorkspaceToolchain(t, s)
			called := false
			node.Pkg.OnBuild(func(ctx *api.BuildContext) {
				called = true
				for name, expected := range map[string]string{"input.c": "int value = 2;\n", ".config": "CONFIG_READY=y\n"} {
					path := filepath.Join(node.Pkg.SrcDir(), "nested", name)
					if data, err := os.ReadFile(path); err != nil || string(data) != expected {
						t.Errorf("OnBuild reads %s = %q, %v", path, data, err)
					}
				}
				if k.SrcDir() != filepath.Join(node.Pkg.SrcDir(), "nested") {
					t.Errorf("Kconfig remained bound to %s, source is %s", k.SrcDir(), node.Pkg.SrcDir())
				}
				ctx.Target("app").SetKind(api.TargetVoid)
			})
			if err := s.executeOnBuild(); err != nil {
				t.Fatal(err)
			}
			if !called || s.pkgDirs["app"].BuildDir == oldDirs.BuildDir {
				t.Fatal("OnBuild did not execute in a changed workspace")
			}
			if data, err := os.ReadFile(filepath.Join(seed, "nested", "input.c")); err != nil || string(data) != "int value = 1;\n" {
				t.Fatalf("source seed changed: %q, %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(seed, "nested", ".config")); !os.IsNotExist(err) {
				t.Fatalf("Kconfig was restored into the source seed: %v", err)
			}
			if info, err := os.Stat(oldConfig); err != nil || !info.ModTime().Equal(mtime) {
				t.Fatalf("previous workspace configuration was rewritten: %v", err)
			}
		})
	}
}

func TestSessionUnchangedWorkspacePreservesConfigMtime(t *testing.T) {
	for _, remote := range []bool{false, true} {
		name := "local-setgit"
		if remote {
			name = "remote"
		}
		t.Run(name, func(t *testing.T) {
			s, k, _ := workspaceBindingFixture(t, remote)
			configPath := filepath.Join(k.SrcDir(), k.ConfigPath())
			mtime := time.Unix(1700000000, 0)
			if err := os.Chtimes(configPath, mtime, mtime); err != nil {
				t.Fatal(err)
			}
			oldDirs := *s.pkgDirs["app"]
			if _, err := s.bindPackage("app"); err != nil {
				t.Fatal(err)
			}
			if *s.pkgDirs["app"] != oldDirs {
				t.Fatal("unchanged toolchain moved the workspace")
			}
			if info, err := os.Stat(configPath); err != nil || !info.ModTime().Equal(mtime) {
				t.Fatalf("unchanged configuration was rewritten: %v", err)
			}
		})
	}
}

func TestSessionWorkspaceRebindFailureStopsOnBuild(t *testing.T) {
	for _, failure := range []string{"patch", "kconfig"} {
		t.Run(failure, func(t *testing.T) {
			s, k, _ := workspaceBindingFixture(t, true)
			node := s.ctx.DepGraph.Packages["app"]
			want := "restore kconfig app"
			if failure == "patch" {
				want = "apply patches for app"
				path := filepath.Join(node.Pkg.ScriptDir(), "change.patch")
				if err := os.WriteFile(path, []byte("invalid patch\n"), 0644); err != nil {
					t.Fatal(err)
				}
			} else {
				k.SetConfigPath("blocked/.config")
			}
			switchWorkspaceToolchain(t, s)
			called := false
			node.Pkg.OnBuild(func(*api.BuildContext) { called = true })
			if err := s.executeOnBuild(); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("preparation failure = %v, want %s", err, want)
			}
			if called {
				t.Fatal("OnBuild executed after workspace preparation failed")
			}
		})
	}
}

func nativeSiblingBindingFixture(t *testing.T, configSource string) (*buildPhaseState, *api.KConfigEntry, string, string) {
	t.Helper()
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	s := sessionFixture(t)
	name, owner, member := "native/root/member", "native/root", "member"
	node := s.ctx.DepGraph.Packages["app"]
	delete(s.ctx.DepGraph.Packages, "app")
	s.ctx.DepGraph.Packages[name] = node
	s.ctx.DepGraph.Order = []string{name}
	s.ctx.Resolver.SubParents()[name] = owner
	s.needed = map[string]bool{name: true}
	s.allPkgOptions[name] = nil
	versionDir := t.TempDir()
	seed := filepath.Join(versionDir, "src")
	for _, dir := range []string{member, "shared"} {
		if err := os.MkdirAll(filepath.Join(seed, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for path, data := range map[string]string{"member/build.go": "package main\n", "shared/input.c": "original\n"} {
		if err := os.WriteFile(filepath.Join(seed, filepath.FromSlash(path)), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	packageRoot := filepath.Join(seed, member)
	node.ID = name
	node.Source = buildscript.NewSource(name, filepath.Join(packageRoot, "build.go"), packageRoot, api.SourceRemote)
	node.Pkg.SetRepo("native").SetName("root/member").SetScriptDir(packageRoot).SetSrcDir("../shared")
	s.remote.entries[owner] = &config.EntryConfig{Version: "1.0.0"}
	s.remote.versionDirs[owner], s.remote.versionDirs[name] = versionDir, versionDir
	s.remote.commits[owner] = "commit"
	tools, err := s.toolsForPackage(name)
	if err != nil {
		t.Fatal(err)
	}
	scriptHash, err := s.scriptHashFor(name)
	if err != nil {
		t.Fatal(err)
	}
	s.pkgDirs[name] = makeRemotePkgDirs(versionDir, packageRoot, tools.CCKey(), s.cfg.Mode, nil, "1.0.0", "commit", packageFlagsHash(s.globalFlagsHash, node), "", scriptHash, member)
	configRoot := filepath.Join(seed, "shared")
	if configSource == "relative" {
		configRoot = "../shared"
	} else if configSource == "external" {
		configRoot = t.TempDir()
	}
	k := (&api.KConfigEntry{}).SetSrcDir(configRoot).SetConfigPath(".config")
	s.ctx.AllKConfigs = map[string][]*api.KConfigEntry{name: {k}}
	config.SetEntry(s.ctx.Config, name, &config.EntryConfig{KConfig: "CONFIG_READY=y\n"})
	if err := s.preparePackageWorkspace(name); err != nil {
		t.Fatal(err)
	}
	if err := s.restoreKConfigs(); err != nil {
		t.Fatal(err)
	}
	return s, k, seed, name
}

func TestSetupSubPackageDirsKeepsResolvedParentCommit(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	s := sessionFixture(t)
	owner, name, member := "native/root", "native/root/member", "member"
	versionDir := t.TempDir()
	seed := filepath.Join(versionDir, "src")
	packageRoot := filepath.Join(seed, member)
	if err := os.MkdirAll(packageRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "build.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	node := s.ctx.DepGraph.Packages["app"]
	delete(s.ctx.DepGraph.Packages, "app")
	delete(s.needed, "app")
	node.ID = name
	node.Source = buildscript.NewSource(name, filepath.Join(packageRoot, "build.go"), packageRoot, api.SourceRemote)
	s.ctx.DepGraph.Packages[name] = node
	s.ctx.DepGraph.Packages[owner] = resolver.NewPackageNode(owner,
		buildscript.NewSource(owner, filepath.Join(seed, "build.go"), seed, api.SourceRemote), api.NewPackage())
	s.ctx.DepGraph.Order = []string{owner, name}
	s.needed[name] = true
	s.needed[owner] = true
	s.allPkgOptions[name] = nil
	s.ctx.Resolver.SubParents()[name] = owner
	s.remote.entries[owner] = &config.EntryConfig{Version: "1.0.0"}
	s.remote.versionDirs[owner] = versionDir
	s.remote.commits[owner] = "resolved-commit"
	config.SetEntry(s.ctx.Config, owner, &config.EntryConfig{Version: "1.0.0"})

	if err := s.setupSubPackageDirs(s.ctx.Paths.DepsDir); err != nil {
		t.Fatal(err)
	}
	if s.remote.commits[owner] != "resolved-commit" || s.remote.commits[name] != "resolved-commit" {
		t.Fatalf("commits = %q/%q, want the resolved commit for both", s.remote.commits[owner], s.remote.commits[name])
	}
}

func TestSessionNativeSiblingKConfigRebindBeforeOnBuild(t *testing.T) {
	for _, source := range []string{"relative", "seed", "external"} {
		t.Run(source, func(t *testing.T) {
			s, k, seed, name := nativeSiblingBindingFixture(t, source)
			node := s.ctx.DepGraph.Packages[name]
			previous := k.SrcDir()
			oldConfig := filepath.Join(previous, k.ConfigPath())
			mtime := time.Unix(1700000000, 0)
			if err := os.Chtimes(oldConfig, mtime, mtime); err != nil {
				t.Fatal(err)
			}
			switchWorkspaceToolchain(t, s)
			called := false
			node.Pkg.OnBuild(func(ctx *api.BuildContext) {
				called = true
				want := filepath.Join(filepath.Dir(s.pkgDirs[name].SourceDir), "shared")
				if node.Pkg.SrcDir() != want {
					t.Errorf("source = %s, want %s", node.Pkg.SrcDir(), want)
				}
				if source == "external" {
					want = previous
				}
				if k.SrcDir() != want {
					t.Errorf("Kconfig source = %s, want %s", k.SrcDir(), want)
				}
				if data, err := os.ReadFile(filepath.Join(k.SrcDir(), ".config")); err != nil || string(data) != "CONFIG_READY=y\n" {
					t.Errorf("OnBuild Kconfig = %q, %v", data, err)
				}
				if err := os.WriteFile(filepath.Join(node.Pkg.SrcDir(), "generated.h"), []byte("generated\n"), 0644); err != nil {
					t.Error(err)
				}
				ctx.Target("ready").SetKind(api.TargetVoid)
			})
			if err := s.executeOnBuild(); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("OnBuild was not called")
			}
			for _, file := range []string{".config", "generated.h"} {
				if _, err := os.Stat(filepath.Join(seed, "shared", file)); !os.IsNotExist(err) {
					t.Fatalf("source seed contains generated %s: %v", file, err)
				}
			}
			if info, err := os.Stat(oldConfig); err != nil || !info.ModTime().Equal(mtime) {
				t.Fatalf("previous config mtime changed: %v", err)
			}
			currentConfig := filepath.Join(k.SrcDir(), ".config")
			if err := os.Chtimes(currentConfig, mtime, mtime); err != nil {
				t.Fatal(err)
			}
			if err := s.restoreKConfigs(); err != nil {
				t.Fatal(err)
			}
			if info, err := os.Stat(currentConfig); err != nil || !info.ModTime().Equal(mtime) {
				t.Fatalf("unchanged config mtime changed: %v", err)
			}
		})
	}
}

func TestSessionNativeKConfigRebaseErrorStopsOnBuild(t *testing.T) {
	s, _, _, name := nativeSiblingBindingFixture(t, "seed")
	s.pkgDirs[name].SourceDir = filepath.Dir(s.pkgDirs[name].SourceDir)
	switchWorkspaceToolchain(t, s)
	called := false
	s.ctx.DepGraph.Packages[name].Pkg.OnBuild(func(*api.BuildContext) { called = true })
	if err := s.executeOnBuild(); err == nil || !strings.Contains(err.Error(), "rebase kconfig "+name) {
		t.Fatalf("Kconfig mapping error = %v", err)
	}
	if called {
		t.Fatal("OnBuild executed after source mapping failed")
	}
}
