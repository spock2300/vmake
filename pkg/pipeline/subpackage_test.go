package pipeline

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/resolver"
)

func addSubPackageNode(s *buildPhaseState, name, owner, dir string) *resolver.PackageNode {
	node := resolver.NewPackageNode(name,
		buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, api.SourceRemote),
		api.NewPackage().SetName(name))
	s.ctx.DepGraph.Packages[name] = node
	s.ctx.DepGraph.Order = append(s.ctx.DepGraph.Order, name)
	if owner != "" {
		s.ctx.Resolver.SubParents()[name] = owner
	}
	return node
}

func TestComputeDepOutputResolvesSubPackageShortName(t *testing.T) {
	s := sessionFixture(t)
	s.allTargets = make(map[string]map[string]*api.Target)
	owner, tool, lib := "native/root", "native/root/tool", "native/root/lib"
	toolDir := filepath.Join(t.TempDir(), "tool")
	libDir := filepath.Join(t.TempDir(), "lib")
	addSubPackageNode(s, owner, "", filepath.Join(t.TempDir(), "root"))
	addSubPackageNode(s, tool, owner, toolDir)
	addSubPackageNode(s, lib, owner, libDir)
	s.needed[owner] = true
	s.needed[tool] = true
	s.needed[lib] = true
	s.pkgDirs[tool] = &api.PkgDirs{SourceDir: toolDir, BuildDir: filepath.Join(t.TempDir(), "tool-out")}
	s.pkgDirs[lib] = &api.PkgDirs{SourceDir: libDir, BuildDir: filepath.Join(t.TempDir(), "lib-out")}
	target := api.NewTargetRegistry().Target("utils").SetKind(api.TargetStatic)
	s.allTargets[lib] = map[string]*api.Target{"utils": target}

	want := filepath.Join(s.pkgDirs[lib].BuildDir,
		api.TargetFilename(api.TargetStatic, "utils", s.cfg.Platform.OSOrHost()))
	if got := s.computeDepOutput(tool, "lib:utils"); got != want {
		t.Fatalf("short target ref = %q, want %q", got, want)
	}
	if got := s.computeDepOutput(tool, lib+":utils"); got != want {
		t.Fatalf("full target ref = %q, want %q", got, want)
	}
	if got := s.computeDepOutput(tool, "lib"); got != want {
		t.Fatalf("short bare ref = %q, want %q", got, want)
	}
	if got := s.computeDepOutput(tool, "missing:utils"); got != "" {
		t.Fatalf("missing package = %q, want empty", got)
	}
	if got := s.computeDepOutput(tool, "lib:missing"); got != "" {
		t.Fatalf("missing target = %q, want empty", got)
	}
}

func TestComputeDepOutputPrefersNearestSubPackage(t *testing.T) {
	s := sessionFixture(t)
	s.allTargets = make(map[string]map[string]*api.Target)
	owner, tool, near, far := "native/root", "native/root/tool", "native/root/lib", "lib"
	addSubPackageNode(s, owner, "", filepath.Join(t.TempDir(), "root"))
	addSubPackageNode(s, tool, owner, filepath.Join(t.TempDir(), "tool"))
	addSubPackageNode(s, near, owner, filepath.Join(t.TempDir(), "near"))
	addSubPackageNode(s, far, "", filepath.Join(t.TempDir(), "far"))
	for _, name := range []string{owner, tool, near, far} {
		s.needed[name] = true
	}
	s.pkgDirs[tool] = &api.PkgDirs{SourceDir: t.TempDir(), BuildDir: t.TempDir()}
	s.pkgDirs[near] = &api.PkgDirs{SourceDir: t.TempDir(), BuildDir: t.TempDir()}
	s.pkgDirs[far] = &api.PkgDirs{SourceDir: t.TempDir(), BuildDir: t.TempDir()}
	target := api.NewTargetRegistry().Target("utils").SetKind(api.TargetStatic)
	s.allTargets[near] = map[string]*api.Target{"utils": target}
	s.allTargets[far] = map[string]*api.Target{"utils": target}

	got := s.computeDepOutput(tool, "lib:utils")
	want := filepath.Join(s.pkgDirs[near].BuildDir,
		api.TargetFilename(api.TargetStatic, "utils", s.cfg.Platform.OSOrHost()))
	if got != want {
		t.Fatalf("nearest ref = %q, want %q", got, want)
	}
}

func TestApplyBuildContextConfigResolvesSubPackageImports(t *testing.T) {
	s := sessionFixture(t)
	owner, tool, lib := "native/root", "native/root/tool", "native/root/lib"
	addSubPackageNode(s, owner, "", filepath.Join(t.TempDir(), "root"))
	toolNode := addSubPackageNode(s, tool, owner, filepath.Join(t.TempDir(), "tool"))
	libPkg := api.NewPackage().SetName(lib)
	cfgCtx := api.NewConfigContext(lib)
	cfgCtx.Option("feature").SetType(api.OptionBool).SetDefault(true)
	libPkg.Options = cfgCtx.GetOptions()
	libPkg.CfgVals = map[string]any{"feature": true}
	s.ctx.DepGraph.Packages[lib] = resolver.NewPackageNode(lib,
		buildscript.NewSource(lib, "build.go", t.TempDir(), api.SourceRemote), libPkg)
	s.ctx.Resolver.SubParents()[lib] = owner
	near := tool + "/lib"
	nearPkg := api.NewPackage().SetName(near)
	nearCfg := api.NewConfigContext(near)
	nearCfg.Option("feature").SetType(api.OptionBool).SetDefault(false)
	nearPkg.Options = nearCfg.GetOptions()
	nearPkg.CfgVals = map[string]any{"feature": false}
	s.ctx.DepGraph.Packages[near] = resolver.NewPackageNode(near,
		buildscript.NewSource(near, "build.go", t.TempDir(), api.SourceRemote), nearPkg)
	s.ctx.Resolver.SubParents()[near] = owner
	needed := map[string]bool{owner: true, tool: true, lib: true}

	for _, defines := range []bool{false, true} {
		t.Run(map[bool]string{false: "header-only", true: "defines"}[defines], func(t *testing.T) {
			node := resolver.NewPackageNode(tool, toolNode.Source, api.NewPackage().SetName(tool))
			buildCtx := api.NewBuildContext(tool, nil)
			buildCtx.SetPackage(node.Pkg)
			target := buildCtx.Target("app").SetKind(api.TargetStatic)
			buildCtx.GenerateConfigHeader()
			if defines {
				buildCtx.GenerateConfigDefines()
			}
			buildCtx.ImportConfig("lib")
			applyBuildContextConfig(buildCtx, node, s.ctx, tool, needed)

			if got := node.Pkg.ImportConfigs(); !reflect.DeepEqual(got, []string{lib}) {
				t.Fatalf("import configs = %v, want [%s]", got, lib)
			}
			if defines {
				if got := target.Defines(); !reflect.DeepEqual(got, []string{"CONFIG_FEATURE=1"}) {
					t.Fatalf("defines = %v, want [CONFIG_FEATURE=1]", got)
				}
			} else if got := target.Defines(); len(got) != 0 {
				t.Fatalf("header-only path injected defines: %v", got)
			}
			if !node.Pkg.GenConfigHeader() {
				t.Fatal("config header flag was not persisted")
			}
		})
	}
}

func TestWriteLockfileSkipsSubPackages(t *testing.T) {
	s := sessionFixture(t)
	owner, sub := "native/root", "native/root/member"
	parent := resolver.NewPackageNode(owner,
		buildscript.NewSource(owner, "build.go", t.TempDir(), api.SourceRemote),
		api.NewPackage().SetName(owner))
	parent.WithNative("https://example.invalid/root.git", map[string]string{"1.0.0": "v1.0.0"}, "1.0.0")
	subNode := resolver.NewPackageNode(sub,
		buildscript.NewSource(sub, "build.go", t.TempDir(), api.SourceRemote),
		api.NewPackage().SetName(sub))
	s.ctx.DepGraph.Packages[owner], s.ctx.DepGraph.Packages[sub] = parent, subNode
	s.ctx.DepGraph.Order = []string{owner, sub}
	s.ctx.Resolver.SubParents()[sub] = owner
	s.remote.entries[owner] = &config.EntryConfig{Version: "1.0.0"}
	s.remote.commits[owner] = "abc123"
	s.ctx.LockPath = filepath.Join(t.TempDir(), "vmake.lock")
	s.ctx.Lock = lockfile.New()
	s.ctx.Lock.Set(sub, &lockfile.LockedPkg{Version: "1.0.0", Commit: "stale", Source: "native"})

	if err := s.writeLockfile(); err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Load(s.ctx.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := locked.Get(owner)
	if !ok || entry.Version != "1.0.0" || entry.Commit != "abc123" || entry.Source != "native" {
		t.Fatalf("parent lock = %+v", entry)
	}
	if _, ok := locked.Get(sub); ok {
		t.Fatal("sub-package must not be locked separately")
	}
}

func TestSetupSubPackageDirsNestedMember(t *testing.T) {
	s := sessionFixture(t)
	owner, sub, member := "native/root", "native/root/a/b", "a/b"
	tree := remoteTreeDir(s.ctx, owner)
	packageRoot := filepath.Join(tree, "src", filepath.FromSlash(member))
	if err := writePipelineFile(packageRoot, "build.go"); err != nil {
		t.Fatal(err)
	}
	parent := resolver.NewPackageNode(owner,
		buildscript.NewSource(owner, filepath.Join(tree, "src", "build.go"), filepath.Join(tree, "src"), api.SourceRemote),
		api.NewPackage().SetName(owner))
	parent.WithNative("unused", map[string]string{"1.0.0": "v1.0.0"}, "1.0.0")
	node := resolver.NewPackageNode(sub,
		buildscript.NewSource(sub, filepath.Join(packageRoot, "build.go"), packageRoot, api.SourceRemote),
		api.NewPackage().SetName(sub))
	s.ctx.DepGraph.Packages[owner], s.ctx.DepGraph.Packages[sub] = parent, node
	config.SetEntry(s.ctx.Config, owner, &config.EntryConfig{Version: "1.0.0"})
	s.ctx.DepGraph.Order = []string{owner, sub}
	s.ctx.Resolver.SubParents()[sub] = owner
	s.needed[owner], s.needed[sub] = true, true
	s.allPkgOptions[sub] = nil
	s.remote.entries[owner] = &config.EntryConfig{Version: "1.0.0"}
	s.remote.commits[owner] = "resolved-commit"
	s.remote.trees[owner] = tree

	if err := s.setupSubPackageDirs(); err != nil {
		t.Fatal(err)
	}
	if got := s.sourceVersions[sub]; got != "1.0.0" {
		t.Fatalf("sub version = %q, want 1.0.0", got)
	}
	if got := s.remote.commits[sub]; got != "resolved-commit" {
		t.Fatalf("sub commit = %q, want resolved-commit", got)
	}
	if got := s.remote.trees[sub]; got != tree {
		t.Fatalf("sub tree = %q, want %q", got, tree)
	}
	dirs := s.pkgDirs[sub]
	if dirs == nil || dirs.SourceDir != packageRoot {
		t.Fatalf("sub dirs = %+v, want source %s", dirs, packageRoot)
	}
	if !strings.HasPrefix(dirs.BuildDir, filepath.Join(tree, "out")+string(filepath.Separator)) {
		t.Fatalf("sub BuildDir = %s, want under %s", dirs.BuildDir, filepath.Join(tree, "out"))
	}
}

func TestSetupSubPackageDirsRequiresParentVersion(t *testing.T) {
	s := sessionFixture(t)
	owner, sub := "native/root", "native/root/member"
	tree := remoteTreeDir(s.ctx, owner)
	packageRoot := filepath.Join(tree, "src", "member")
	if err := writePipelineFile(packageRoot, "build.go"); err != nil {
		t.Fatal(err)
	}
	parent := resolver.NewPackageNode(owner,
		buildscript.NewSource(owner, filepath.Join(tree, "src", "build.go"), filepath.Join(tree, "src"), api.SourceRemote),
		api.NewPackage().SetName(owner))
	node := resolver.NewPackageNode(sub,
		buildscript.NewSource(sub, filepath.Join(packageRoot, "build.go"), packageRoot, api.SourceRemote),
		api.NewPackage().SetName(sub))
	s.ctx.DepGraph.Packages[owner], s.ctx.DepGraph.Packages[sub] = parent, node
	s.ctx.DepGraph.Order = []string{owner, sub}
	s.ctx.Resolver.SubParents()[sub] = owner
	s.needed[owner], s.needed[sub] = true, true
	s.allPkgOptions[sub] = nil

	err := s.setupSubPackageDirs()
	if err == nil || !strings.Contains(err.Error(), "has no resolved version") {
		t.Fatalf("setup error = %v, want missing parent version", err)
	}
}

func TestRemoteOwnerAndMemberPathNested(t *testing.T) {
	r := resolver.NewResolver(nil, t.TempDir())
	r.SubParents()["native/root/a/b"] = "native/root"
	ctx := &RuntimeContext{Resolver: r}
	if got := remoteOwnerName(ctx, "native/root/a/b"); got != "native/root" {
		t.Fatalf("owner = %q, want native/root", got)
	}
	if got := remoteMemberPath(ctx, "native/root/a/b"); got != "a/b" {
		t.Fatalf("member = %q, want a/b", got)
	}
	if got := remoteMemberPath(ctx, "native/root"); got != "" {
		t.Fatalf("root member = %q, want empty", got)
	}
}

func TestFilterAndCollectNeededRejectsSubPackageWithoutParent(t *testing.T) {
	s := sessionFixture(t)
	owner, sub := "native/root", "native/root/member"
	parent := resolver.NewPackageNode(owner,
		buildscript.NewSource(owner, "build.go", t.TempDir(), api.SourceRemote),
		api.NewPackage().SetName(owner))
	subNode := resolver.NewPackageNode(sub,
		buildscript.NewSource(sub, "build.go", t.TempDir(), api.SourceRemote),
		api.NewPackage().SetName(sub))
	s.ctx.DepGraph.Packages[owner], s.ctx.DepGraph.Packages[sub] = parent, subNode
	s.ctx.Resolver.SubParents()[sub] = owner
	s.ctx.DepGraph.Packages["app"].Pkg.SetRoot(true)
	s.ctx.DepGraph.Packages["app"].Deps = []string{sub}

	_, err := filterAndCollectNeeded(s.ctx)
	if err == nil || !strings.Contains(err.Error(), "native parent native/root is not") {
		t.Fatalf("needed sub-package without parent error = %v", err)
	}
}

func TestNeededSubPackagesRequireNeededParent(t *testing.T) {
	subParents := map[string]string{
		"native/root/a":   "native/root",
		"native/root/b":   "native/root",
		"native/root/c/d": "native/root/c",
	}
	if err := validateNeededSubPackageOwners(map[string]bool{"native/root/a": true}, subParents); err == nil ||
		!strings.Contains(err.Error(), "native/root/a") || !strings.Contains(err.Error(), "native/root") {
		t.Fatalf("missing parent validation error = %v", err)
	}
	if err := validateNeededSubPackageOwners(map[string]bool{"native/root/a": true, "native/root": true}, subParents); err != nil {
		t.Fatalf("needed parent rejected: %v", err)
	}
	if err := validateNeededSubPackageOwners(map[string]bool{"native/root/c/d": true, "native/root/c": true}, subParents); err != nil {
		t.Fatalf("needed nested parent rejected: %v", err)
	}
}

func TestSessionBuildRequestResolvesSubPackageShortName(t *testing.T) {
	s, tree, memberA := nativeMemberFixture(t, "member_a", false)
	owner := "native/root"
	memberB := owner + "/member_b"
	dirB := filepath.Join(tree, "src", "member_b")
	if err := writePipelineFile(dirB, "build.go"); err != nil {
		t.Fatal(err)
	}
	nodeB := resolver.NewPackageNode(memberB,
		buildscript.NewSource(memberB, filepath.Join(dirB, "build.go"), dirB, api.SourceRemote),
		api.NewPackage().SetName(memberB))
	s.ctx.DepGraph.Packages[memberB] = nodeB
	s.ctx.DepGraph.Order = []string{owner, memberA, memberB, "app"}
	s.ctx.Resolver.SubParents()[memberB] = owner
	s.needed[memberB] = true
	s.remote.commits[memberB] = s.remote.commits[owner]
	s.computeDirsAndOptions()

	builds := 0
	nodeB.Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("b").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error {
			builds++
			return nil
		})
	})
	s.ctx.DepGraph.Packages[memberA].Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.BuildSubGraph("member_b")
		ctx.Target("a").SetKind(api.TargetVoid)
	})

	if err := s.executeOnBuild(); err != nil {
		t.Fatal(err)
	}
	if builds != 1 {
		t.Fatalf("member_b built %d times, want 1", builds)
	}
}

func TestSessionBuildRequestSkipsFilteredOutCandidate(t *testing.T) {
	s, tree, current := nativeMemberFixture(t, "member_a", false)
	owner := "native/root"
	near := current + "/lib"
	far := owner + "/lib"
	nearDir := filepath.Join(tree, "src", "member_a", "lib")
	farDir := filepath.Join(tree, "src", "lib")
	for _, dir := range []string{nearDir, farDir} {
		if err := writePipelineFile(dir, "build.go"); err != nil {
			t.Fatal(err)
		}
	}
	addSubPackageNode(s, near, owner, nearDir)
	addSubPackageNode(s, far, owner, farDir)
	s.needed[far] = true
	s.remote.commits[far] = s.remote.commits[owner]
	s.computeDirsAndOptions()

	nearBuilds, farBuilds := 0, 0
	s.ctx.DepGraph.Packages[near].Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("lib").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error {
			nearBuilds++
			return nil
		})
	})
	s.ctx.DepGraph.Packages[far].Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("lib").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error {
			farBuilds++
			return nil
		})
	})
	s.ctx.DepGraph.Packages[current].Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.BuildSubGraph("lib")
		ctx.Target("a").SetKind(api.TargetVoid)
	})

	if err := s.executeOnBuild(); err != nil {
		t.Fatal(err)
	}
	if farBuilds != 1 || nearBuilds != 0 {
		t.Fatalf("near builds=%d, far builds=%d, want 0/1", nearBuilds, farBuilds)
	}
}

func TestSessionMissingBuildPackageFails(t *testing.T) {
	s := sessionFixture(t)
	owner, tool, lib := "native/root", "native/root/tool", "native/root/lib"
	addSubPackageNode(s, owner, "", filepath.Join(t.TempDir(), "root"))
	addSubPackageNode(s, tool, owner, filepath.Join(t.TempDir(), "tool"))
	addSubPackageNode(s, lib, owner, filepath.Join(t.TempDir(), "lib"))
	s.needed[owner] = true
	s.needed[tool] = true

	err := s.buildSubGraph(tool, "lib")
	if err == nil || !strings.Contains(err.Error(), "subgraph package lib not found in required packages") {
		t.Fatalf("short-name build request error = %v, want missing short name", err)
	}
}

func writePipelineFile(dir, name string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), []byte("package main\n"), 0644)
}
