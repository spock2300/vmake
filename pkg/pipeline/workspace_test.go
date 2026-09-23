package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

func nativeMemberLinkFixture(t *testing.T, member string, setGit bool) (*buildPhaseState, string, string) {
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
	versionDir := remoteVersionDir(s.ctx, owner, "1.0.0")
	seed := filepath.Join(versionDir, "src")
	for name, content := range map[string]string{
		"build.go":                           "package main\n",
		filepath.Join(member, "build.go"):    "package main\n",
		filepath.Join(member, "payload.txt"): "original",
	} {
		path := filepath.Join(seed, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := buildscript.ScanSubPackages(seed, owner)
	if err != nil || len(sources) != 1 {
		t.Fatalf("native member discovery = %+v, %v", sources, err)
	}
	source := &sources[0]
	name := owner + "/" + member
	if source.Name != name {
		t.Fatalf("discovered member = %s, want %s", source.Name, name)
	}
	parent := resolver.NewPackageNode(owner, buildscript.NewSource(owner, filepath.Join(seed, "build.go"), seed, api.SourceRemote), api.NewPackage())
	parent.WithNative("unused", nil, "1.0.0")
	parent.Native.Commit = "owner-commit"
	node := resolver.NewPackageNode(name, source, api.NewPackage())
	if setGit {
		upstream := t.TempDir()
		if err := os.WriteFile(filepath.Join(upstream, "payload.txt"), []byte("original"), 0644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "source"}} {
			cmd := exec.Command("git", gitcmd.Args(args...)...)
			cmd.Dir = upstream
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %s: %v", args, output, err)
			}
		}
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
	s.remote.versionDirs[owner], s.remote.commits[owner] = versionDir, parent.Native.Commit
	for _, dir := range []string{"src", "out"} {
		target := filepath.Join(versionDir, dir)
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatal(err)
		}
		if err := fs.EnsureSymlink(filepath.Join(s.ctx.Paths.DepsDir, filepath.FromSlash(owner), dir), target); err != nil {
			t.Fatal(err)
		}
	}
	return s, seed, name
}

func sourceTreeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		value := info.Mode().String() + "\x00" + info.ModTime().UTC().String()
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += "\x00" + string(data)
		} else if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += "\x00" + target
		}
		files[rel] = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestNativeMemberSourceLinksPreserveSeedAndParentLinks(t *testing.T) {
	for _, member := range []string{"src", "src/sub", "out", "out/sub", "normal", "nested/member", "_members/src"} {
		for _, setGit := range []bool{false, true} {
			label := member
			if setGit {
				label += "/setgit"
			}
			t.Run(label, func(t *testing.T) {
				s, seed, name := nativeMemberLinkFixture(t, member, setGit)
				before := sourceTreeSnapshot(t, seed)
				if err := s.setupSubPackageDirs(s.ctx.Paths.DepsDir); err != nil {
					t.Fatal(err)
				}
				if err := s.cloneSubPackageGitSources(); err != nil {
					t.Fatal(err)
				}
				dirs := s.pkgDirs[name]
				link := nativeMemberSourceLink(s.ctx, name)
				target := dirs.SourceDir
				if setGit {
					target = filepath.Join(target, "src")
				}
				if got, err := os.Readlink(link); err != nil || got != target {
					t.Fatalf("member source link = %s, %v; want %s", got, err, target)
				}
				path := filepath.Join(link, "payload.txt")
				if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
					t.Fatalf("member source = %q, %v", data, err)
				}
				if err := os.WriteFile(path, []byte("workspace change"), 0644); err != nil {
					t.Fatal(err)
				}
				mtime := time.Unix(1700000000, 0)
				if err := os.Chtimes(path, mtime, mtime); err != nil {
					t.Fatal(err)
				}
				if err := s.preparePackageWorkspace(name); err != nil {
					t.Fatal(err)
				}
				if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(mtime) {
					t.Fatalf("unchanged workspace mtime changed: %v", err)
				}
				if data, err := os.ReadFile(path); err != nil || string(data) != "workspace change" {
					t.Fatalf("existing workspace was replaced: %q, %v", data, err)
				}
				if after := sourceTreeSnapshot(t, seed); !reflect.DeepEqual(before, after) {
					t.Fatalf("immutable seed changed: before=%v after=%v", before, after)
				}
				for _, dir := range []string{"src", "out"} {
					link := filepath.Join(s.ctx.Paths.DepsDir, "native", "root", dir)
					if got, err := os.Readlink(link); err != nil || got != filepath.Join(filepath.Dir(seed), dir) {
						t.Fatalf("parent %s link changed: %s, %v", dir, got, err)
					}
				}
			})
		}
	}
}

func TestNativeMembersUseIndependentWholeRepositoryWorkspaces(t *testing.T) {
	versionDir := t.TempDir()
	seed := filepath.Join(versionDir, "src")
	for name, content := range map[string]string{"shared/value.h": "original", "sub_a/build.go": "a", "sub_b/build.go": "b"} {
		path := filepath.Join(seed, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	manager := repo.NewSourceManager(t.TempDir(), t.TempDir())
	dirs := make(map[string]*api.PkgDirs)
	for _, member := range []string{"sub_a", "sub_b"} {
		src := buildscript.NewSource("native/root/"+member, filepath.Join(seed, member, "build.go"), filepath.Join(seed, member), api.SourceRemote)
		node := resolver.NewPackageNode(src.Name, src, api.NewPackage())
		dirs[member] = makeRemotePkgDirs(versionDir, src.Dir, "cc", "debug", nil, "1.0.0", "commit", "", "", "script", member)
		if err := prepareRemoteWorkspace(manager, node, dirs[member], versionDir, "commit", ""); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(dirs[member].BuildDir, seed+string(filepath.Separator)) {
			t.Fatal("build output is inside immutable seed")
		}
		data, err := os.ReadFile(filepath.Join(dirs[member].SourceDir, "..", "shared", "value.h"))
		if err != nil || string(data) != "original" {
			t.Fatalf("sibling source is unavailable: %q, %v", data, err)
		}
	}
	if dirs["sub_a"].BuildDir == dirs["sub_b"].BuildDir {
		t.Fatal("members share build directory")
	}
	if err := os.WriteFile(filepath.Join(dirs["sub_a"].SourceDir, "..", "shared", "value.h"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(seed, "shared", "value.h"), filepath.Join(dirs["sub_b"].SourceDir, "..", "shared", "value.h")} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "original" {
			t.Fatalf("member write leaked into %s: %q, %v", path, data, err)
		}
	}
}

func TestLocalSetGitWorkspacePreservesSrcRelativePaths(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip(fs.SymlinkHint)
	}
	upstream := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", gitcmd.Args(args...)...)
		cmd.Dir = upstream
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, output, err)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "test")
	git("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(upstream, "input.c"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", "input.c")
	git("commit", "-q", "-m", "source")
	project := t.TempDir()
	pkg := api.NewPackage().SetName("local").SetGit(upstream)
	src := buildscript.NewSource("local", filepath.Join(project, "build.go"), project, api.SourceLocal)
	r := resolver.NewResolver(repo.NewRepoManager(t.TempDir()), t.TempDir())
	r.Graph().Packages["local"] = resolver.NewPackageNode("local", src, pkg)
	ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Paths: &Paths{DepsDir: t.TempDir(), CacheDir: t.TempDir()}}
	s := newBuildPhaseState(ctx, BuildOptions{})
	s.pkgDirs = map[string]*api.PkgDirs{"local": {SourceDir: project, BuildDir: filepath.Join(project, "build", "key-one")}}
	if err := s.preparePackageWorkspace("local"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(project, "src", "input.c")); err != nil || string(data) != "original" {
		t.Fatalf("SourceDir/src compatibility failed: %q, %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(pkg.SrcDir(), "input.c"), []byte("private"), 0644); err != nil {
		t.Fatal(err)
	}
	s.pkgDirs["local"].BuildDir = filepath.Join(project, "build", "key-two")
	if err := s.preparePackageWorkspace("local"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(project, "src", "input.c"), filepath.Join(s.sourceSeeds["local"], "input.c"), filepath.Join(upstream, "input.c")} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "original" {
			t.Fatalf("source mutation leaked into %s: %q, %v", path, data, err)
		}
	}
}

func TestRemoteWorkspaceRebindingMovesExplicitSourcePath(t *testing.T) {
	versionDir := t.TempDir()
	seed := filepath.Join(versionDir, "src")
	if err := os.MkdirAll(filepath.Join(seed, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	pkg := api.NewPackage().SetSrcDir(filepath.Join(seed, "nested"))
	node := resolver.NewPackageNode("native/sample", buildscript.NewSource("native/sample", filepath.Join(seed, "build.go"), seed, api.SourceRemote), pkg)
	manager := repo.NewSourceManager(t.TempDir(), t.TempDir())
	first := makeRemotePkgDirs(versionDir, seed, "cc-one", "debug", nil, "1.0", "commit", "", "", "")
	if err := prepareRemoteWorkspace(manager, node, first, versionDir, "commit", ""); err != nil {
		t.Fatal(err)
	}
	if pkg.SrcDir() != filepath.Join(first.SourceDir, "nested") {
		t.Fatalf("first source binding = %s", pkg.SrcDir())
	}
	second := makeRemotePkgDirs(versionDir, seed, "cc-two", "debug", nil, "1.0", "commit", "", "", "")
	if err := prepareRemoteWorkspace(manager, node, second, versionDir, "commit", ""); err != nil {
		t.Fatal(err)
	}
	if pkg.SrcDir() != filepath.Join(second.SourceDir, "nested") || pkg.SrcDir() == filepath.Join(first.SourceDir, "nested") {
		t.Fatalf("source remained bound to previous toolchain workspace: %s", pkg.SrcDir())
	}
}

func TestNativeWorkspaceRebasesRepositorySiblingSources(t *testing.T) {
	for _, source := range []string{"relative", "seed", "seed-alias", "external"} {
		t.Run(source, func(t *testing.T) {
			versionDir := t.TempDir()
			seed := filepath.Join(versionDir, "src")
			member := filepath.Join("nested", "member")
			for _, dir := range []string{member, "shared"} {
				if err := os.MkdirAll(filepath.Join(seed, dir), 0755); err != nil {
					t.Fatal(err)
				}
			}
			input := filepath.Join(seed, "shared", "input.c")
			if err := os.WriteFile(input, []byte("original"), 0644); err != nil {
				t.Fatal(err)
			}
			mtime := time.Unix(1700000000, 0)
			if err := os.Chtimes(input, mtime, mtime); err != nil {
				t.Fatal(err)
			}
			packageRoot := filepath.Join(seed, member)
			raw := filepath.Join("..", "..", "shared")
			if source == "seed" || source == "seed-alias" {
				raw = filepath.Join(seed, "shared")
			}
			if source == "seed-alias" {
				if !fs.SymlinksSupported() {
					t.Skip(fs.SymlinkHint)
				}
				alias := filepath.Join(t.TempDir(), "source")
				if err := os.Symlink(seed, alias); err != nil {
					t.Fatal(err)
				}
				packageRoot = filepath.Join(alias, member)
			}
			if source == "external" {
				raw = t.TempDir()
			}
			pkg := api.NewPackage().SetSrcDir(raw).SetDirs(api.PkgDirs{SourceDir: packageRoot})
			node := resolver.NewPackageNode("native/sample/nested/member", buildscript.NewSource("native/sample/nested/member", filepath.Join(packageRoot, "build.go"), packageRoot, api.SourceRemote), pkg)
			manager := repo.NewSourceManager(t.TempDir(), t.TempDir())
			var previous string
			for _, cc := range []string{"cc-one", "cc-two"} {
				dirs := makeRemotePkgDirs(versionDir, packageRoot, cc, "debug", nil, "1.0", "commit", "", "", "", member)
				if err := prepareRemoteWorkspace(manager, node, dirs, versionDir, "commit", ""); err != nil {
					t.Fatal(err)
				}
				want := filepath.Join(filepath.Dir(dirs.BuildDir), "work", "repo", "shared")
				if source == "external" {
					want = raw
				}
				if pkg.SrcDir() != want {
					t.Fatalf("%s source = %s, want %s", cc, pkg.SrcDir(), want)
				}
				if source == "external" {
					continue
				}
				path := filepath.Join(pkg.SrcDir(), "input.c")
				if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
					t.Fatalf("workspace source = %q, %v", data, err)
				}
				if err := os.WriteFile(path, []byte(cc), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, mtime, mtime); err != nil {
					t.Fatal(err)
				}
				if err := prepareRemoteWorkspace(manager, node, dirs, versionDir, "commit", ""); err != nil {
					t.Fatal(err)
				}
				for checked, content := range map[string]string{input: "original", path: cc, previous: "cc-one"} {
					if checked == "" {
						continue
					}
					if data, err := os.ReadFile(checked); err != nil || string(data) != content {
						t.Fatalf("source isolation %s = %q, %v", checked, data, err)
					}
					if info, err := os.Stat(checked); err != nil || !info.ModTime().Equal(mtime) {
						t.Fatalf("unchanged source mtime %s: %v", checked, err)
					}
				}
				previous = path
			}
		})
	}
}

func TestRemoteWorkspaceRejectsInvalidSourceMapping(t *testing.T) {
	for _, failure := range []string{"member-outside-repository", "member-mismatch", "relative-package-root"} {
		t.Run(failure, func(t *testing.T) {
			versionDir := t.TempDir()
			seed := filepath.Join(versionDir, "src")
			if err := os.MkdirAll(filepath.Join(seed, "member"), 0755); err != nil {
				t.Fatal(err)
			}
			pkg := api.NewPackage().SetSrcDir(filepath.Join(seed, "shared"))
			node := resolver.NewPackageNode("native/sample/member", buildscript.NewSource("native/sample/member", filepath.Join(seed, "member", "build.go"), filepath.Join(seed, "member"), api.SourceRemote), pkg)
			dirs := makeRemotePkgDirs(versionDir, node.Source.Dir, "cc", "debug", nil, "1.0", "commit", "", "", "", "member")
			switch failure {
			case "member-outside-repository":
				dirs.SourceDir = seed
			case "member-mismatch":
				pkg.SetDirs(api.PkgDirs{SourceDir: filepath.Join(seed, "different-member")})
			case "relative-package-root":
				pkg.SetDirs(api.PkgDirs{SourceDir: "relative/member"})
			}
			manager := repo.NewSourceManager(t.TempDir(), t.TempDir())
			if err := prepareRemoteWorkspace(manager, node, dirs, versionDir, "commit", ""); err == nil {
				t.Fatal("invalid source mapping was accepted")
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(dirs.BuildDir), "work")); !os.IsNotExist(err) {
				t.Fatalf("invalid source mapping materialized a workspace: %v", err)
			}
			if pkg.SrcDirRaw() != filepath.Join(seed, "shared") {
				t.Fatal("failed source mapping changed package source")
			}
		})
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

func TestLocalSetGitRefSelectionAndLock(t *testing.T) {
	upstream := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", gitcmd.Args(args...)...)
		cmd.Dir = upstream
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, output, err)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "test")
	git("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(upstream, "input.c"), []byte("version one"), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", "input.c")
	git("commit", "-q", "-m", "one")
	git("tag", "v1.0.0")
	first, err := repo.GetCurrentCommit(upstream)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(upstream, "input.c"), []byte("version two"), 0644); err != nil {
		t.Fatal(err)
	}
	git("commit", "-qam", "two")
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
