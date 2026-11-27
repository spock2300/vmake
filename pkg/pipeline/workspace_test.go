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
	node := s.ctx.DepGraph.Packages["//app"]
	node.Pkg.SetGit(upstream).AddVersion("1.0.0", "v1.0.0")
	node.Constraints = []string{"=1.0.0"}
	if err := s.selectPackageSource("//app"); err != nil {
		t.Fatal(err)
	}
	s.pkgDirs["//app"] = makeLocalPkgDirs(node.Source.Dir, localPkgKeyFromParts("cc-key", s.cfg.Mode, nil, "", s.sourceCommits["//app"]))
	if err := s.preparePackageWorkspace("//app"); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(node.Source.Dir, "src")
	want := filepath.Join(s.ctx.Paths.DepsDir, "local", build.MemberDir("app"), "src")
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
	s.pkgDirs["//app"].BuildDir = filepath.Join(node.Source.Dir, "build", "other-key")
	if err := s.preparePackageWorkspace("//app"); err != nil {
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
	node := s.ctx.DepGraph.Packages["//app"]
	node.Pkg.SetGit(upstream).AddVersion("1.0.0", "v1.0.0").AddVersion("2.0.0", "main")
	node.Constraints = []string{"=1.0.0"}
	s.ctx.LockPath = filepath.Join(s.ctx.Paths.ProjectDir, "vmake.lock")
	if err := s.selectPackageSource("//app"); err != nil {
		t.Fatal(err)
	}
	if s.sourceVersions["//app"] != "1.0.0" || s.sourceCommits["//app"] != first {
		t.Fatalf("source selection = %s/%s", s.sourceVersions["//app"], s.sourceCommits["//app"])
	}
	if err := s.writeLockfile(); err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Load(s.ctx.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := locked.GetSource("//app")
	if !ok || entry.Version != "1.0.0" || entry.Commit != first {
		t.Fatalf("local git lock = %+v", entry)
	}
	s.ctx.Lock = locked
	node.Constraints = nil
	s.ctx.LockPolicy = resolver.LockPolicy{Float: map[string]bool{"//app": true}}
	updated := newBuildPhaseState(s.ctx, BuildOptions{})
	if err := updated.selectPackageSource("//app"); err != nil {
		t.Fatal(err)
	}
	if updated.sourceVersions["//app"] != "2.0.0" || updated.sourceCommits["//app"] == first {
		t.Fatalf("deps update retained previous selection: %s/%s", updated.sourceVersions["//app"], updated.sourceCommits["//app"])
	}
	s.ctx.LockPolicy = resolver.LockPolicy{}
	// Bring the tree back to the locked version while the source is still
	// reachable; a locked build must then reuse it without the network.
	back := newBuildPhaseState(s.ctx, BuildOptions{})
	if err := back.selectPackageSource("//app"); err != nil {
		t.Fatal(err)
	}
	if back.sourceCommits["//app"] != first || back.sourceVersions["//app"] != "1.0.0" {
		t.Fatalf("online lock selected %s/%s", back.sourceVersions["//app"], back.sourceCommits["//app"])
	}
	if err := os.Rename(upstream, filepath.Join(t.TempDir(), "offline")); err != nil {
		t.Fatal(err)
	}
	next := newBuildPhaseState(s.ctx, BuildOptions{})
	if err := next.selectPackageSource("//app"); err != nil {
		t.Fatal(err)
	}
	if next.sourceCommits["//app"] != first || next.sourceVersions["//app"] != "1.0.0" {
		t.Fatalf("offline lock selected %s/%s", next.sourceVersions["//app"], next.sourceCommits["//app"])
	}
	node.Constraints = []string{"=2.0.0"}
	if err := newBuildPhaseState(s.ctx, BuildOptions{}).selectPackageSource("//app"); err == nil {
		t.Fatal("incompatible source constraint bypassed lock")
	}
}

func TestLocalSetGitPreservesUnmanagedSourceDirectory(t *testing.T) {
	s := sessionFixture(t)
	node := s.ctx.DepGraph.Packages["//app"]
	node.Pkg.SetGit("unavailable-upstream")
	dir := filepath.Join(node.Source.Dir, "src")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "private.c")
	if err := os.WriteFile(path, []byte("private source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.preparePackageWorkspace("//app"); err == nil || !strings.Contains(err.Error(), "real directory") {
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
	containerID := "native/root"
	tree := remoteTreeDir(s.ctx, containerID)
	for name, content := range map[string]string{
		"vmake.json":                         "{}\n",
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
	name := containerID + "/" + member
	memberDir := filepath.Join(tree, "src", filepath.FromSlash(member))
	source := buildscript.NewSource(name, filepath.Join(memberDir, "build.go"), memberDir, api.SourceRemote)
	source.Container = containerID
	source.RelPath = member
	s.ctx.DepGraph.Containers[containerID] = &resolver.ContainerNode{
		ID:      containerID,
		Repo:    "native",
		Kind:    resolver.SourceKindNative,
		Dir:     filepath.Join(tree, "src"),
		URL:     "unused",
		Version: "1.0.0",
		Commit:  "owner-commit",
	}
	node := resolver.NewPackageNode(name, containerID, member, resolver.SourceKindNative, source, api.NewPackage().SetName(name))
	if setGit {
		upstream := initGitRepo(t, map[string]string{"payload.txt": "original"})
		node.Pkg.SetGit(upstream)
	}
	s.ctx.DepGraph.Packages[name] = node
	delete(s.ctx.DepGraph.Packages, "//dep")
	s.ctx.DepGraph.Order = []string{name, "//app"}
	s.ctx.DepGraph.Roots = []string{"//app"}
	s.ctx.DepGraph.Packages["//app"].Deps = []string{name}
	s.needed = map[string]bool{"//app": true, name: true}
	s.cfg = makeBuildConfig(s.ctx, host, "host")
	aggregateGlobalFlags(s.ctx, s.needed)
	s.globalFlagsHash = build.HashGlobalFlags([4][]string{})
	s.computeDirsAndOptions(t)
	s.containerTrees[containerID] = tree
	s.containerCommits[containerID] = "owner-commit"
	s.containerVersions[containerID] = "1.0.0"
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
			if err := s.preparePackages(&sourceDownloadLog{}); err != nil {
				t.Fatal(err)
			}
			dirs := s.pkgDirs[name]
			if want := filepath.Join(tree, "src", "member"); dirs.SourceDir != want {
				t.Fatalf("member SourceDir = %s, want %s", dirs.SourceDir, want)
			}
			if !strings.HasPrefix(dirs.BuildDir, filepath.Join(tree, "out")+string(filepath.Separator)) {
				t.Fatalf("member BuildDir = %s, want under %s", dirs.BuildDir, filepath.Join(tree, "out"))
			}
			node := s.ctx.DepGraph.Packages[name]
			tools, err := s.toolsForPackage(name)
			if err != nil {
				t.Fatal(err)
			}
			wantExtra := build.JoinKeyExtra(
				"1.0.0",
				sourceCommitKey(s.containerCommits["native/root"], s.sourceCommits[name]),
				packageFlagsHash(s.globalFlagsHash, node),
				s.patchHashes[name],
			)
			wantKey := build.BuildKey(tools.CCKey(), s.cfg.Mode, s.allPkgOptions[name], wantExtra)
			if got := filepath.Base(filepath.Dir(dirs.BuildDir)); got != build.KeyDir(wantKey) {
				t.Fatalf("member BuildDir key = %q, want %q", got, wantKey)
			}
			insp, err := Inspect(s.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := insp.pkgDirs[name].BuildDir, dirs.BuildDir; got != want {
				t.Fatalf("inspect BuildDir = %q, want build %q", got, want)
			}
			if setGit {
				wantSrc := filepath.Join(tree, "third_party", build.MemberDir("member"), "src")
				if got := s.ctx.DepGraph.Packages[name].Pkg.SrcDir(); got != wantSrc {
					t.Fatalf("member SrcDir = %s, want %s", got, wantSrc)
				}
				if data, err := os.ReadFile(filepath.Join(wantSrc, "payload.txt")); err != nil || string(data) != "original" {
					t.Fatalf("own-git member content = %q, %v", data, err)
				}
				// Container src/ tree must NOT get polluted with a src symlink.
				if _, err := os.Lstat(filepath.Join(dirs.SourceDir, "src")); !os.IsNotExist(err) {
					t.Fatalf("own-git member created symlink inside container tree: %v", err)
				}
			}
			// Sibling paths in the container repository stay reachable.
			if data, err := os.ReadFile(filepath.Join(dirs.SourceDir, "..", "vmake.json")); err != nil || string(data) != "{}\n" {
				t.Fatalf("container root unavailable: %q, %v", data, err)
			}
		})
	}
}

func TestPreparePackagesKeepsResolvedContainerCommit(t *testing.T) {
	s := sessionFixture(t)
	containerID, name, member := "native/root", "native/root/member", "member"
	tree := remoteTreeDir(s.ctx, containerID)
	packageRoot := filepath.Join(tree, "src", member)
	if err := os.MkdirAll(packageRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "build.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	delete(s.ctx.DepGraph.Packages, "//app")
	delete(s.ctx.DepGraph.Packages, "//dep")
	s.ctx.DepGraph.Order = nil
	s.ctx.DepGraph.Containers[containerID] = &resolver.ContainerNode{
		ID:      containerID,
		Repo:    "native",
		Kind:    resolver.SourceKindNative,
		Dir:     filepath.Join(tree, "src"),
		Version: "1.0.0",
		Commit:  "resolved-commit",
	}
	addNativeMemberNode(s, containerID, member, packageRoot)
	s.needed = map[string]bool{name: true}
	s.allPkgOptions[name] = nil
	s.containerVersions[containerID] = "1.0.0"
	s.containerTrees[containerID] = tree
	s.containerCommits[containerID] = "resolved-commit"
	s.ctx.Pins = map[string]string{containerID: "1.0.0"}

	if err := s.preparePackages(&sourceDownloadLog{}); err != nil {
		t.Fatal(err)
	}
	if s.containerCommits[containerID] != "resolved-commit" {
		t.Fatalf("container commit = %q, want resolved-commit", s.containerCommits[containerID])
	}
	if s.containerTrees[containerID] != tree {
		t.Fatalf("container tree = %q, want %q", s.containerTrees[containerID], tree)
	}
	tools, err := s.toolsForPackage(name)
	if err != nil {
		t.Fatal(err)
	}
	wantExtra := build.JoinKeyExtra(
		"1.0.0",
		sourceCommitKey(s.containerCommits[containerID], s.sourceCommits[name]),
		packageFlagsHash(s.globalFlagsHash, s.ctx.DepGraph.Packages[name]),
		s.patchHashes[name],
	)
	want := build.BuildKey(tools.CCKey(), s.cfg.Mode, s.allPkgOptions[name], wantExtra)
	if got := filepath.Base(filepath.Dir(s.pkgDirs[name].BuildDir)); got != build.KeyDir(want) {
		t.Fatalf("member BuildDir key = %q, want %q", got, want)
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

func TestPrepareAllPackagesBindsDirsBeforePatchApply(t *testing.T) {
	upstream := initGitRepo(t, map[string]string{
		"build.go": "package main\n",
		"input.c":  "int value = 1;\n",
	})
	patch := "--- a/input.c\n+++ b/input.c\n@@ -1 +1 @@\n-int value = 1;\n+int value = 2;\n"
	scriptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(scriptDir, "change.patch"), []byte(patch), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(scriptDir, "followup.patch"), []byte("--- a/input.c\n+++ b/input.c\n@@ -1 +1 @@\n-int value = 2;\n+int value = 3;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s := sessionFixture(t)
	delete(s.ctx.DepGraph.Packages, "//app")
	delete(s.ctx.DepGraph.Packages, "//dep")
	s.ctx.DepGraph.Order = []string{"official/sample"}
	s.needed = map[string]bool{"official/sample": true}
	s.ctx.DepGraph.Containers["official"] = &resolver.ContainerNode{
		ID:   "official",
		Repo: "official",
		Kind: resolver.SourceKindRegistry,
	}
	src := buildscript.NewSource("official/sample", filepath.Join(scriptDir, "build.go"), scriptDir, api.SourceRemote)
	src.Container = "official"
	src.RelPath = "sample"
	node := resolver.NewPackageNode("official/sample", "official", "sample", resolver.SourceKindRegistry, src,
		api.NewPackage().SetRepo("official").SetName("sample"))
	node.Pkg.SetScriptDir(scriptDir).AddPatches("change.patch", "followup.patch").SetGit(upstream)
	s.ctx.DepGraph.Packages["official/sample"] = node

	if err := s.prepareAllPackages(); err != nil {
		t.Fatal(err)
	}
	if err := s.applyPatchesToNeeded(); err != nil {
		t.Fatal(err)
	}
	if err := s.applyPatchesToNeeded(); err != nil {
		t.Fatalf("reuse ordered patch series: %v", err)
	}
	dirs := s.pkgDirs["official/sample"]
	if node.Pkg.SourceDir() != dirs.SourceDir {
		t.Fatalf("source dir = %q, want %q", node.Pkg.SourceDir(), dirs.SourceDir)
	}
	data, err := os.ReadFile(filepath.Join(node.Pkg.SrcDir(), "input.c"))
	if err != nil || string(data) != "int value = 3;\n" {
		t.Fatalf("patched content = %q, %v", data, err)
	}
}

func TestApplyPatchesToOrdinaryLocalPackageAndRejectUserManaged(t *testing.T) {
	s := sessionFixture(t)
	delete(s.ctx.DepGraph.Packages, "//dep")
	s.ctx.DepGraph.Order = []string{"//app"}
	s.needed = map[string]bool{"//app": true}

	appNode := s.ctx.DepGraph.Packages["//app"]
	if err := os.WriteFile(filepath.Join(appNode.Source.Dir, "input.c"), []byte("int value = 1;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testGit(t, s.ctx.Paths.ProjectDir, "init", "-q")
	testGit(t, s.ctx.Paths.ProjectDir, "config", "user.email", "test@example.com")
	testGit(t, s.ctx.Paths.ProjectDir, "config", "user.name", "test")
	testGit(t, s.ctx.Paths.ProjectDir, "add", "-A")
	testGit(t, s.ctx.Paths.ProjectDir, "commit", "-q", "-m", "init")
	patch := "--- a/input.c\n+++ b/input.c\n@@ -1 +1 @@\n-int value = 1;\n+int value = 2;\n"
	if err := os.WriteFile(filepath.Join(appNode.Source.Dir, "local.patch"), []byte(patch), 0644); err != nil {
		t.Fatal(err)
	}
	appNode.Pkg.SetScriptDir(appNode.Source.Dir).AddPatches("local.patch")

	if err := s.prepareAllPackages(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.applyPatchesToNeeded(); err != nil {
			t.Fatalf("apply local patches in place: %v", err)
		}
	}
	data, err := os.ReadFile(filepath.Join(appNode.Source.Dir, "input.c"))
	if err != nil || string(data) != "int value = 2;\n" {
		t.Fatalf("ordinary local patched content = %q, %v", data, err)
	}

	pathContainer := "fixture/localmod"
	pathDir := t.TempDir()
	s.ctx.DepGraph.Containers[pathContainer] = &resolver.ContainerNode{
		ID:        pathContainer,
		Repo:      "fixture",
		Kind:      resolver.SourceKindNative,
		Dir:       pathDir,
		Version:   resolver.LocalPathVersion,
		LocalPath: true,
	}
	modNode := addNativeMemberNode(s, pathContainer, "", pathDir)
	modNode.Pkg.SetScriptDir(appNode.Source.Dir).AddPatches("local.patch")
	s.needed[pathContainer] = true
	if err := s.prepareAllPackages(); err != nil {
		t.Fatal(err)
	}
	if err := s.applyPatchesToNeeded(); err == nil || !strings.Contains(err.Error(), "user-managed") {
		t.Fatalf("expected user-managed error for local-path container, got %v", err)
	}
}

func TestContainerTreeIdentityIncludesSharedMemberPatches(t *testing.T) {
	s := sessionFixture(t)
	containerID, member := "native/root", "native/root/member"
	scriptDir := t.TempDir()
	patchPath := filepath.Join(scriptDir, "change.patch")
	writePatch := func(body string) {
		t.Helper()
		if err := os.WriteFile(patchPath, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writePatch("first\n")

	parent := addNativeMemberNode(s, containerID, "", scriptDir)
	memberNode := addNativeMemberNode(s, containerID, "member", scriptDir)
	memberNode.Pkg.SetScriptDir(scriptDir).AddPatches("change.patch")
	s.needed = map[string]bool{containerID: true, member: true}

	first, _, err := s.containerTreeIdentity(containerID)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || s.patchHashes[member] == "" {
		t.Fatalf("shared member patch was ignored: tree=%q member=%q", first, s.patchHashes[member])
	}

	writePatch("second\n")
	second, _, err := s.containerTreeIdentity(containerID)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("changed member patch kept the tree identity")
	}

	// A member with its own repository owns a separate tree; its patches must
	// not change the container tree identity.
	memberNode.Pkg.SetGit("file:///nonexistent")
	if ownGit, _, err := s.containerTreeIdentity(containerID); err != nil || ownGit != "" {
		t.Fatalf("own-git member changed the container tree identity: %q, %v", ownGit, err)
	}
	memberNode.Pkg.SetGit()
	memberNode.Pkg.SetPatches()

	parent.Pkg.SetScriptDir(scriptDir).AddPatches("change.patch")
	own, _, err := s.containerTreeIdentity(containerID)
	if err != nil || own == "" {
		t.Fatalf("root-only patch identity = %q, %v", own, err)
	}
	memberNode.Pkg.AddPatches("change.patch")
	combined, _, err := s.containerTreeIdentity(containerID)
	if err != nil || combined == own {
		t.Fatalf("shared patch did not extend the container identity: %q vs %q, %v", combined, own, err)
	}
}

func TestPrepareContainersUsesSharedTreePatchIdentity(t *testing.T) {
	upstream := initGitRepo(t, map[string]string{
		"vmake.json":      "{}\n",
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
	delete(s.ctx.DepGraph.Packages, "//app")
	delete(s.ctx.DepGraph.Packages, "//dep")
	s.ctx.DepGraph.Order = nil
	containerID, member := "native/root", "native/root/member"
	s.ctx.DepGraph.Containers[containerID] = &resolver.ContainerNode{
		ID:       containerID,
		Repo:     "native",
		Kind:     resolver.SourceKindNative,
		Dir:      upstream,
		URL:      upstream,
		Versions: map[string]string{"1.0.0": "v1.0.0"},
		Version:  "1.0.0",
	}
	memberNode := addNativeMemberNode(s, containerID, "member", filepath.Join(upstream, "member"))
	memberNode.Pkg.SetScriptDir(scriptDir).AddPatches("change.patch")
	s.needed = map[string]bool{member: true}

	if err := s.prepareAllPackages(); err != nil {
		t.Fatal(err)
	}
	want, _, err := s.containerTreeIdentity(containerID)
	if err != nil || want == "" {
		t.Fatalf("container tree patch identity = %q, %v", want, err)
	}
	manager := repo.NewSourceManager(s.ctx.Paths.DepsDir, s.ctx.Paths.CacheDir)
	state, err := manager.ReadState(s.containerTrees[containerID])
	if err != nil || state == nil || state.PatchHash != want {
		t.Fatalf("recorded tree patch hash = %#v, %v; want %q", state, err, want)
	}
}

func TestResolvePackageSourceVersionForOwnGitMember(t *testing.T) {
	s := sessionFixture(t)
	dir := t.TempDir()
	name := "native/root/member"
	node := addNativeMemberNode(s, "native/root", "member", dir)
	node.Pkg.SetGit("https://example.invalid/third.git").AddVersion("1.2.0", "v1.2.0").AddVersion("2.0.0", "v2.0.0")
	s.ctx.Pins = map[string]string{name: "1.2.0"}

	version, ref, commit, err := s.resolvePackageSourceVersion(name, node)
	if err != nil {
		t.Fatal(err)
	}
	if version != "1.2.0" || ref != "v1.2.0" || commit != "" {
		t.Fatalf("own-git member version = %q/%q/%q, want 1.2.0/v1.2.0/empty", version, ref, commit)
	}

	nonGit := addNativeMemberNode(s, "native/root", "plain", dir)
	nonGit.Pkg.AddVersion("3.0.0", "v3.0.0")
	version, ref, commit, err = s.resolvePackageSourceVersion("native/root/plain", nonGit)
	if err != nil {
		t.Fatal(err)
	}
	if version != "" || ref != "" || commit != "" {
		t.Fatalf("non-SetGit member version = %q/%q/%q, want empty", version, ref, commit)
	}
}
