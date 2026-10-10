package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitcmd"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func testGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", gitcmd.Args(args...)...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %s: %v", args, dir, output, err)
	}
}

func initGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	testGit(t, dir, "init", "-q", "-b", "main")
	testGit(t, dir, "config", "user.name", "test")
	testGit(t, dir, "config", "user.email", "test@example.com")
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	testGit(t, dir, "add", "-A")
	testGit(t, dir, "commit", "-q", "-m", "source")
	return dir
}

func TestLocalSetGitSingleTreeStableLink(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	upstream := initGitRepo(t, map[string]string{"input.c": "original\n"})
	testGit(t, upstream, "tag", "v1.0.0")

	s := sessionFixture(t)
	node := s.ctx.DepGraph.Packages["app"]
	node.Pkg.SetGit(upstream).AddVersion("1.0.0", "v1.0.0")
	node.Constraints = []string{"=1.0.0"}
	if err := s.selectPackageSource("app"); err != nil {
		t.Fatal(err)
	}
	s.pkgDirs["app"] = makeLocalPkgDirs(node.Source.Dir, "cc-key", s.cfg.Mode, nil, "", "", s.sourceCommits["app"])
	if err := s.preparePackageWorkspace("app"); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(node.Source.Dir, "src")
	want := filepath.Join(s.ctx.Paths.DepsDir, "local", "app", "src")
	target, err := os.Readlink(link)
	if err != nil || target != want {
		t.Fatalf("src link = %q, %v; want %q", target, err, want)
	}
	if got := node.Pkg.SrcDir(); got != link {
		t.Fatalf("SrcDir = %q, want link %q", got, link)
	}
	if data, err := os.ReadFile(filepath.Join(link, "input.c")); err != nil || string(data) != "original\n" {
		t.Fatalf("linked content = %q, %v", data, err)
	}

	// Local modifications stay in the single tree; a different build key must
	// reuse the same tree instead of re-copying it.
	if err := os.WriteFile(filepath.Join(link, "input.c"), []byte("private\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s.pkgDirs["app"].BuildDir = filepath.Join(node.Source.Dir, "build", "other-key")
	if err := s.preparePackageWorkspace("app"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(link, "input.c")); err != nil || string(data) != "private\n" {
		t.Fatalf("tree was recreated on rebind: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(node.Source.Dir, "build", "cc-key", "work")); !os.IsNotExist(err) {
		t.Fatalf("per-build workspace copy appeared: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(upstream, "input.c")); err != nil || string(data) != "original\n" {
		t.Fatalf("upstream source was modified: %q, %v", data, err)
	}
}

func TestLocalSetGitRefSelectionAndLock(t *testing.T) {
	upstream := initGitRepo(t, map[string]string{"input.c": "version one\n"})
	testGit(t, upstream, "tag", "v1.0.0")
	first, err := repo.GetCurrentCommit(upstream)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(upstream, "input.c"), []byte("version two\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testGit(t, upstream, "commit", "-qam", "two")

	s := sessionFixture(t)
	node := s.ctx.DepGraph.Packages["app"]
	node.Pkg.SetGit(upstream).AddVersion("1.0.0", "v1.0.0").AddVersion("2.0.0", "main")
	node.Constraints = []string{"=1.0.0"}
	s.ctx.LockPath = filepath.Join(s.ctx.Paths.ProjectDir, "vmake.lock")
	if err := s.selectPackageSource("app"); err != nil {
		t.Fatal(err)
	}
	if s.sourceVersions["app"] != "1.0.0" || s.sourceCommits["app"] != first {
		t.Fatalf("source selection = %s/%s", s.sourceVersions["app"], s.sourceCommits["app"])
	}
	if err := s.writeLockfile(); err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Load(s.ctx.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := locked.Get("app")
	if !ok || entry.Version != "1.0.0" || entry.Commit != first || entry.Source != "git" {
		t.Fatalf("local git lock = %+v", entry)
	}
	s.ctx.Lock = locked
	node.Constraints = nil
	s.ctx.IgnoreLock = true
	updated := newBuildPhaseState(s.ctx, BuildOptions{})
	if err := updated.selectPackageSource("app"); err != nil {
		t.Fatal(err)
	}
	if updated.sourceVersions["app"] != "2.0.0" || updated.sourceCommits["app"] == first {
		t.Fatalf("lock update retained previous selection: %s/%s", updated.sourceVersions["app"], updated.sourceCommits["app"])
	}
	s.ctx.IgnoreLock = false
	// Bring the tree back to the locked version while the source is still
	// reachable; a locked build must then reuse it without the network.
	back := newBuildPhaseState(s.ctx, BuildOptions{})
	if err := back.selectPackageSource("app"); err != nil {
		t.Fatal(err)
	}
	if back.sourceCommits["app"] != first || back.sourceVersions["app"] != "1.0.0" {
		t.Fatalf("online lock selected %s/%s", back.sourceVersions["app"], back.sourceCommits["app"])
	}
	if err := os.Rename(upstream, filepath.Join(t.TempDir(), "offline")); err != nil {
		t.Fatal(err)
	}
	next := newBuildPhaseState(s.ctx, BuildOptions{})
	if err := next.selectPackageSource("app"); err != nil {
		t.Fatal(err)
	}
	if next.sourceCommits["app"] != first || next.sourceVersions["app"] != "1.0.0" {
		t.Fatalf("offline lock selected %s/%s", next.sourceVersions["app"], next.sourceCommits["app"])
	}
	node.Constraints = []string{"=2.0.0"}
	if err := newBuildPhaseState(s.ctx, BuildOptions{}).selectPackageSource("app"); err == nil {
		t.Fatal("incompatible source constraint bypassed lock")
	}
}

func TestLocalSetGitPreservesUnmanagedSourceDirectory(t *testing.T) {
	s := sessionFixture(t)
	node := s.ctx.DepGraph.Packages["app"]
	node.Pkg.SetGit("unavailable-upstream")
	dir := filepath.Join(node.Source.Dir, "src")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "private.c")
	if err := os.WriteFile(path, []byte("private source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.preparePackageWorkspace("app"); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("unmanaged source directory was accepted: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "private source" {
		t.Fatalf("unmanaged source was changed: %q, %v", data, err)
	}
}

func nativeMemberFixture(t *testing.T, member string, setGit bool) (*buildPhaseState, string, string) {
	t.Helper()
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	host, err := toolchain.GetManager().GetToolchain("host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.ResolveTools(host, api.Platform{}); err != nil {
		t.Skipf("host tools unavailable: %v", err)
	}
	s := sessionFixture(t)
	owner := "native/root"
	tree := remoteTreeDir(s.ctx, owner)
	for name, content := range map[string]string{
		"build.go":                           "package main\n",
		filepath.Join(member, "build.go"):    "package main\n",
		filepath.Join(member, "payload.txt"): "original",
	} {
		path := filepath.Join(tree, "src", name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := buildscript.ScanSubPackages(filepath.Join(tree, "src"), owner)
	if err != nil || len(sources) != 1 {
		t.Fatalf("native member discovery = %+v, %v", sources, err)
	}
	source := &sources[0]
	name := owner + "/" + member
	if source.Name != name {
		t.Fatalf("discovered member = %s, want %s", source.Name, name)
	}
	parent := resolver.NewPackageNode(owner, buildscript.NewSource(owner, filepath.Join(tree, "src", "build.go"), filepath.Join(tree, "src"), api.SourceRemote), api.NewPackage())
	parent.WithNative("unused", nil, "1.0.0")
	parent.Native.Commit = "owner-commit"
	node := resolver.NewPackageNode(name, source, api.NewPackage())
	if setGit {
		upstream := initGitRepo(t, map[string]string{"payload.txt": "original"})
		node.Pkg.SetGit(upstream)
	}
	s.ctx.DepGraph.Packages[owner], s.ctx.DepGraph.Packages[name] = parent, node
	delete(s.ctx.DepGraph.Packages, "dep")
	s.ctx.DepGraph.Order = []string{owner, name, "app"}
	s.ctx.Resolver.SubParents()[name] = owner
	s.ctx.DepGraph.Packages["app"].Pkg.SetRoot(true)
	s.ctx.DepGraph.Packages["app"].Deps = []string{owner, name}
	s.needed = map[string]bool{"app": true, owner: true, name: true}
	s.cfg = makeBuildConfig(s.ctx, host, "host")
	applyGlobalFlagsFromNeeded(s.ctx, s.needed)
	s.globalFlagsHash = build.GlobalFlagsHash()
	s.computeDirsAndOptions()
	s.remote.trees[owner] = tree
	s.remote.commits[owner] = parent.Native.Commit
	return s, tree, name
}

func TestNativeMemberSingleTreeAndOwnGit(t *testing.T) {
	for _, setGit := range []bool{false, true} {
		label := "shared"
		if setGit {
			label = "own-git"
		}
		t.Run(label, func(t *testing.T) {
			s, tree, name := nativeMemberFixture(t, "member", setGit)
			if err := s.setupSubPackageDirs(); err != nil {
				t.Fatal(err)
			}
			if err := s.cloneSubPackageGitSources(); err != nil {
				t.Fatal(err)
			}
			dirs := s.pkgDirs[name]
			if want := filepath.Join(tree, "src", "member"); dirs.SourceDir != want {
				t.Fatalf("member SourceDir = %s, want %s", dirs.SourceDir, want)
			}
			if !strings.HasPrefix(dirs.BuildDir, filepath.Join(tree, "out")+string(filepath.Separator)) {
				t.Fatalf("member BuildDir = %s, want under %s", dirs.BuildDir, filepath.Join(tree, "out"))
			}
			if setGit {
				if got := s.ctx.DepGraph.Packages[name].Pkg.SrcDir(); got != filepath.Join(dirs.SourceDir, "src") {
					t.Fatalf("member SrcDir = %s, want %s", got, filepath.Join(dirs.SourceDir, "src"))
				}
				if data, err := os.ReadFile(filepath.Join(dirs.SourceDir, "src", "payload.txt")); err != nil || string(data) != "original" {
					t.Fatalf("own-git member content = %q, %v", data, err)
				}
			}
			// Sibling paths in the parent repository stay reachable.
			if data, err := os.ReadFile(filepath.Join(dirs.SourceDir, "..", "build.go")); err != nil || string(data) != "package main\n" {
				t.Fatalf("parent source unavailable: %q, %v", data, err)
			}
		})
	}
}

func TestSetupSubPackageDirsKeepsResolvedParentCommit(t *testing.T) {
	s := sessionFixture(t)
	owner, name, member := "native/root", "native/root/member", "member"
	tree := remoteTreeDir(s.ctx, owner)
	packageRoot := filepath.Join(tree, "src", member)
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
		buildscript.NewSource(owner, filepath.Join(tree, "src", "build.go"), filepath.Join(tree, "src"), api.SourceRemote), api.NewPackage())
	s.ctx.DepGraph.Order = []string{owner, name}
	s.needed[name] = true
	s.needed[owner] = true
	s.allPkgOptions[name] = nil
	s.ctx.Resolver.SubParents()[name] = owner
	s.remote.entries[owner] = &config.EntryConfig{Version: "1.0.0"}
	s.remote.trees[owner] = tree
	s.remote.commits[owner] = "resolved-commit"
	config.SetEntry(s.ctx.Config, owner, &config.EntryConfig{Version: "1.0.0"})

	if err := s.setupSubPackageDirs(); err != nil {
		t.Fatal(err)
	}
	if s.remote.commits[owner] != "resolved-commit" || s.remote.commits[name] != "resolved-commit" {
		t.Fatalf("commits = %q/%q, want the resolved commit for both", s.remote.commits[owner], s.remote.commits[name])
	}
	if s.remote.trees[name] != tree {
		t.Fatalf("member tree = %q, want %q", s.remote.trees[name], tree)
	}
}

func TestRebaseRelativeSourcePathOutsideRepository(t *testing.T) {
	oldRoot := filepath.Join(t.TempDir(), "repo", "member")
	newRoot := filepath.Join(t.TempDir(), "repo", "member")
	for _, path := range []string{"../../outside", "../sibling"} {
		got, err := rebaseSourcePath(path, oldRoot, newRoot, "member")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(newRoot, path)
		if path == "../../outside" {
			want = filepath.Join(oldRoot, path)
		}
		if got != want {
			t.Fatalf("rebase %s = %s, want %s", path, got, want)
		}
	}
	if _, err := rebaseSourcePath("../sibling", "relative/member", newRoot, "member"); err == nil {
		t.Fatal("relative source path bypassed old package directory validation")
	}
}

func TestDownloadRemoteSourcesBindsDirsBeforePatchApply(t *testing.T) {
	upstream := initGitRepo(t, map[string]string{
		"build.go": "package main\n",
		"input.c":  "int value = 1;\n",
	})
	patch := "--- a/input.c\n+++ b/input.c\n@@ -1 +1 @@\n-int value = 1;\n+int value = 2;\n"
	scriptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(scriptDir, "change.patch"), []byte(patch), 0644); err != nil {
		t.Fatal(err)
	}

	s := sessionFixture(t)
	delete(s.ctx.DepGraph.Packages, "app")
	delete(s.ctx.DepGraph.Packages, "dep")
	s.ctx.DepGraph.Order = []string{"native/sample"}
	s.needed = map[string]bool{"native/sample": true}
	node := resolver.NewPackageNode("native/sample",
		buildscript.NewSource("native/sample", filepath.Join(upstream, "build.go"), upstream, api.SourceRemote),
		api.NewPackage().SetRepo("native").SetName("sample"))
	node.Pkg.SetScriptDir(scriptDir).AddPatches("change.patch").SetGit(upstream)
	s.ctx.DepGraph.Packages["native/sample"] = node
	s.remote.entries["native/sample"] = &config.EntryConfig{}

	if err := s.downloadRemoteSources(s.remote, s.ctx.Paths.DepsDir); err != nil {
		t.Fatal(err)
	}
	if err := s.applyPatchesToNeeded(); err != nil {
		t.Fatal(err)
	}
	dirs := s.pkgDirs["native/sample"]
	if node.Pkg.SourceDir() != dirs.SourceDir {
		t.Fatalf("source dir = %q, want %q", node.Pkg.SourceDir(), dirs.SourceDir)
	}
	data, err := os.ReadFile(filepath.Join(node.Pkg.SrcDir(), "input.c"))
	if err != nil || string(data) != "int value = 2;\n" {
		t.Fatalf("patched content = %q, %v", data, err)
	}
}

func TestTreePatchHashIncludesSharedMemberPatches(t *testing.T) {
	s := sessionFixture(t)
	owner, member := "native/root", "native/root/member"
	scriptDir := t.TempDir()
	patchPath := filepath.Join(scriptDir, "change.patch")
	writePatch := func(body string) {
		t.Helper()
		if err := os.WriteFile(patchPath, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writePatch("first\n")

	memberNode := resolver.NewPackageNode(member,
		buildscript.NewSource(member, filepath.Join(scriptDir, "build.go"), scriptDir, api.SourceRemote),
		api.NewPackage().SetName(member))
	memberNode.Pkg.SetScriptDir(scriptDir).AddPatches("change.patch")
	parent := resolver.NewPackageNode(owner,
		buildscript.NewSource(owner, filepath.Join(scriptDir, "build.go"), scriptDir, api.SourceRemote),
		api.NewPackage().SetName(owner))
	s.ctx.DepGraph.Packages[owner], s.ctx.DepGraph.Packages[member] = parent, memberNode
	s.ctx.Resolver.SubParents()[member] = owner
	s.needed = map[string]bool{owner: true, member: true}

	first, err := s.treePatchHash(owner)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || s.patchHashes[member] == "" {
		t.Fatalf("shared member patch was ignored: tree=%q member=%q", first, s.patchHashes[member])
	}

	writePatch("second\n")
	second, err := s.treePatchHash(owner)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("changed member patch kept the tree identity")
	}

	// A member with its own repository owns a separate tree; its patches must
	// not change the parent tree identity.
	memberNode.Pkg.SetGit("file:///nonexistent")
	if ownGit, err := s.treePatchHash(owner); err != nil || ownGit != "" {
		t.Fatalf("own-git member changed the parent tree identity: %q, %v", ownGit, err)
	}
	memberNode.Pkg.SetGit()
	memberNode.Pkg.SetPatches()

	parent.Pkg.SetScriptDir(scriptDir).AddPatches("change.patch")
	own, err := s.treePatchHash(owner)
	if err != nil || own == "" || own != s.patchHashes[owner] {
		t.Fatalf("parent-only patch identity = %q, %v", own, err)
	}
	memberNode.Pkg.AddPatches("change.patch")
	combined, err := s.treePatchHash(owner)
	if err != nil || combined == own {
		t.Fatalf("shared patch did not extend the parent identity: %q vs %q, %v", combined, own, err)
	}
}

func TestDownloadRemoteSourcesUsesSharedTreePatchIdentity(t *testing.T) {
	upstream := initGitRepo(t, map[string]string{
		"build.go":        "package main\n",
		"member/build.go": "package main\n",
		"member/input.c":  "int value = 1;\n",
	})
	testGit(t, upstream, "tag", "v1.0.0")
	patch := "--- a/input.c\n+++ b/input.c\n@@ -1 +1 @@\n-int value = 1;\n+int value = 2;\n"
	scriptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(scriptDir, "change.patch"), []byte(patch), 0644); err != nil {
		t.Fatal(err)
	}

	s := sessionFixture(t)
	delete(s.ctx.DepGraph.Packages, "app")
	delete(s.ctx.DepGraph.Packages, "dep")
	owner, member := "native/root", "native/root/member"
	parent := resolver.NewPackageNode(owner,
		buildscript.NewSource(owner, filepath.Join(upstream, "build.go"), upstream, api.SourceRemote),
		api.NewPackage().SetRepo("native").SetName("root"))
	parent.WithNative(upstream, map[string]string{"1.0.0": "v1.0.0"}, "1.0.0")
	memberNode := resolver.NewPackageNode(member,
		buildscript.NewSource(member, filepath.Join(upstream, "member", "build.go"), filepath.Join(upstream, "member"), api.SourceRemote),
		api.NewPackage().SetRepo("native").SetName("root/member"))
	memberNode.Pkg.SetScriptDir(scriptDir).AddPatches("change.patch")
	s.ctx.DepGraph.Packages[owner], s.ctx.DepGraph.Packages[member] = parent, memberNode
	s.ctx.DepGraph.Order = []string{owner, member}
	s.ctx.Resolver.SubParents()[member] = owner
	s.needed = map[string]bool{owner: true, member: true}
	s.remote.entries[owner] = &config.EntryConfig{Version: "1.0.0"}

	if err := s.downloadRemoteSources(s.remote, s.ctx.Paths.DepsDir); err != nil {
		t.Fatal(err)
	}
	want, err := s.treePatchHash(owner)
	if err != nil || want == "" {
		t.Fatalf("tree patch identity = %q, %v", want, err)
	}
	manager := repo.NewSourceManager(s.ctx.Paths.DepsDir, s.ctx.Paths.CacheDir)
	state, err := manager.ReadState(s.remote.trees[owner])
	if err != nil || state == nil || state.PatchHash != want {
		t.Fatalf("recorded tree patch hash = %#v, %v; want %q", state, err, want)
	}
}
