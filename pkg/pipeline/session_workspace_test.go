package pipeline

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/repo"
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
	upstream := initGitRepo(t, map[string]string{
		"build.go":       "package main\n",
		"nested/input.c": "int value = 1;\n",
		"nested/blocked": "regular file\n",
	})
	patch := "--- a/nested/input.c\n+++ b/nested/input.c\n@@ -1 +1 @@\n-int value = 1;\n+int value = 2;\n"
	if err := os.WriteFile(filepath.Join(node.Source.Dir, "change.patch"), []byte(patch), 0644); err != nil {
		t.Fatal(err)
	}
	node.Pkg.SetScriptDir(node.Source.Dir).AddPatches("change.patch")
	patchHash, err := repo.PatchSetHash(node.Pkg)
	if err != nil {
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
	var configRoot string
	if remote {
		node.Source = buildscript.NewSource("app", filepath.Join(upstream, "build.go"), upstream, api.SourceRemote)
		commit, err := repo.GetCurrentCommit(upstream)
		if err != nil {
			t.Fatal(err)
		}
		s.remote.entries["app"] = &config.EntryConfig{Version: "1.0.0"}
		manager := repo.NewSourceManager(s.ctx.Paths.DepsDir, s.ctx.Paths.CacheDir).WithSession(s.ctx.Locks).WithContext(s.ctx.Context)
		res, err := manager.EnsureSource(repo.SourceRequest{
			Key: "app", URLs: []string{upstream}, Version: "1.0.0", Commit: commit, PatchHash: patchHash,
		})
		if err != nil {
			t.Fatal(err)
		}
		s.remote.trees["app"] = res.Root
		s.remote.commits["app"] = res.Commit
		s.patchHashes["app"] = patchHash
		s.pkgDirs["app"] = makeRemotePkgDirs(res.Root, "", tools.CCKey(), s.cfg.Mode, s.allPkgOptions["app"], "1.0.0", res.Commit, flagsHash, patchHash, scriptHash)
		configRoot = filepath.Join(res.Root, "src", "nested")
	} else {
		node.Pkg.SetGit(upstream)
		s.patchHashes["app"] = patchHash
		if err := s.selectPackageSource("app"); err != nil {
			t.Fatal(err)
		}
		s.pkgDirs["app"] = makeLocalPkgDirs(node.Source.Dir, tools.CCKey(), s.cfg.Mode, s.allPkgOptions["app"], flagsHash, scriptHash, s.sourceCommits["app"])
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
	return s, k, upstream
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

func TestSessionWorkspaceRebindKeepsSingleTree(t *testing.T) {
	for _, remote := range []bool{false, true} {
		name := "local-setgit"
		if remote {
			name = "remote"
		}
		t.Run(name, func(t *testing.T) {
			s, k, seed := workspaceBindingFixture(t, remote)
			node := s.ctx.DepGraph.Packages["app"]
			oldDirs := *s.pkgDirs["app"]
			configPath := filepath.Join(k.SrcDir(), k.ConfigPath())
			mtime := time.Unix(1700000000, 0)
			if err := os.Chtimes(configPath, mtime, mtime); err != nil {
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
			if !called {
				t.Fatal("OnBuild was not called")
			}
			if s.pkgDirs["app"].BuildDir == oldDirs.BuildDir {
				t.Fatal("toolchain change did not move the build directory")
			}
			if s.pkgDirs["app"].SourceDir != oldDirs.SourceDir {
				t.Fatalf("single tree moved: %s -> %s", oldDirs.SourceDir, s.pkgDirs["app"].SourceDir)
			}
			if data, err := os.ReadFile(filepath.Join(seed, "nested", "input.c")); err != nil || string(data) != "int value = 1;\n" {
				t.Fatalf("source seed changed: %q, %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(seed, "nested", ".config")); !os.IsNotExist(err) {
				t.Fatalf("Kconfig was restored into the source seed: %v", err)
			}
			if info, err := os.Stat(configPath); err != nil || !info.ModTime().Equal(mtime) {
				t.Fatalf("unchanged workspace configuration was rewritten: %v", err)
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
	tree := remoteTreeDir(s.ctx, owner)
	for _, dir := range []string{member, "shared"} {
		if err := os.MkdirAll(filepath.Join(tree, "src", dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for path, data := range map[string]string{"member/build.go": "package main\n", "shared/input.c": "original\n"} {
		if err := os.WriteFile(filepath.Join(tree, "src", filepath.FromSlash(path)), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	packageRoot := filepath.Join(tree, "src", member)
	node.ID = name
	node.Source = buildscript.NewSource(name, filepath.Join(packageRoot, "build.go"), packageRoot, api.SourceRemote)
	node.Pkg.SetRepo("native").SetName("root/member").SetScriptDir(packageRoot).SetSrcDir("../shared")
	s.remote.entries[owner] = &config.EntryConfig{Version: "1.0.0"}
	s.remote.trees[owner], s.remote.trees[name] = tree, tree
	s.remote.commits[owner] = "commit"
	tools, err := s.toolsForPackage(name)
	if err != nil {
		t.Fatal(err)
	}
	scriptHash, err := s.scriptHashFor(name)
	if err != nil {
		t.Fatal(err)
	}
	s.pkgDirs[name] = makeRemotePkgDirs(tree, member, tools.CCKey(), s.cfg.Mode, nil, "1.0.0", "commit", packageFlagsHash(s.globalFlagsHash, node), "", scriptHash)
	configRoot := filepath.Join(tree, "src", "shared")
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
	return s, k, tree, name
}

func TestSessionNativeSiblingKConfigRebindBeforeOnBuild(t *testing.T) {
	for _, source := range []string{"relative", "seed", "external"} {
		t.Run(source, func(t *testing.T) {
			s, k, tree, name := nativeSiblingBindingFixture(t, source)
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
				want := filepath.Join(tree, "src", "shared")
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
			if _, err := os.Stat(filepath.Join(k.SrcDir(), k.ConfigPath())); err != nil {
				t.Fatalf("Kconfig missing in the working tree: %v", err)
			}
			if _, err := os.Stat(filepath.Join(node.Pkg.SrcDir(), "generated.h")); err != nil {
				t.Fatalf("generated.h missing in the working tree: %v", err)
			}
			if info, err := os.Stat(oldConfig); err != nil || !info.ModTime().Equal(mtime) {
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
