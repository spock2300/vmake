package pipeline

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/resolver"
)

func addNativeMemberNode(s *buildPhaseState, containerID, rel, dir string) *resolver.PackageNode {
	name := containerID
	if rel != "" {
		name = containerID + "/" + rel
	}
	if s.ctx.DepGraph.Containers[containerID] == nil {
		s.ctx.DepGraph.Containers[containerID] = &resolver.ContainerNode{
			ID:      containerID,
			Repo:    strings.SplitN(containerID, "/", 2)[0],
			Kind:    resolver.SourceKindNative,
			Dir:     dir,
			Version: "1.0.0",
			Commit:  "resolved-commit",
		}
	}
	src := buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, api.SourceRemote)
	src.Container = containerID
	src.RelPath = rel
	node := resolver.NewPackageNode(name, containerID, rel, resolver.SourceKindNative, src, api.NewPackage().SetName(name))
	s.ctx.DepGraph.Packages[name] = node
	s.ctx.DepGraph.Order = append(s.ctx.DepGraph.Order, name)
	return node
}

func TestComputeDepOutputResolvesIntraContainerRef(t *testing.T) {
	s := sessionFixture(t)
	s.allTargets = make(map[string]map[string]*api.Target)
	containerID := "native/root"
	tool := "native/root/tool"
	lib := "native/root/lib"
	toolDir := filepath.Join(canonicalTempDir(t), "tool")
	libDir := filepath.Join(canonicalTempDir(t), "lib")
	addNativeMemberNode(s, containerID, "tool", toolDir)
	addNativeMemberNode(s, containerID, "lib", libDir)
	s.needed[tool] = true
	s.needed[lib] = true
	s.pkgDirs[tool] = &api.PkgDirs{SourceDir: toolDir, BuildDir: filepath.Join(canonicalTempDir(t), "tool-out")}
	s.pkgDirs[lib] = &api.PkgDirs{SourceDir: libDir, BuildDir: filepath.Join(canonicalTempDir(t), "lib-out")}
	target := api.NewTargetRegistry().Target("utils").SetKind(api.TargetStatic)
	s.allTargets[lib] = map[string]*api.Target{"utils": target}

	want := filepath.Join(s.pkgDirs[lib].BuildDir,
		api.TargetFilename(api.TargetStatic, "utils", s.cfg.Platform.OSOrHost()))
	if got := s.computeDepOutput(tool, "//lib:utils"); got != want {
		t.Fatalf("intra-container target ref = %q, want %q", got, want)
	}
	if got := s.computeDepOutput(tool, lib+":utils"); got != want {
		t.Fatalf("full target ref = %q, want %q", got, want)
	}
	if got := s.computeDepOutput(tool, "//lib"); got != want {
		t.Fatalf("intra-container bare ref = %q, want %q", got, want)
	}
	if got := s.computeDepOutput(tool, "//missing:utils"); got != "" {
		t.Fatalf("missing package = %q, want empty", got)
	}
	if got := s.computeDepOutput(tool, "//lib:missing"); got != "" {
		t.Fatalf("missing target = %q, want empty", got)
	}
}

func TestComputeDepSrcDirResolvesPackageSource(t *testing.T) {
	s := sessionFixture(t)
	containerID := "native/root"
	tool := "native/root/tool"
	lib := "native/root/lib"
	toolDir := filepath.Join(canonicalTempDir(t), "tool")
	libDir := filepath.Join(canonicalTempDir(t), "lib")
	addNativeMemberNode(s, containerID, "tool", toolDir)
	libNode := addNativeMemberNode(s, containerID, "lib", libDir)
	s.needed[tool] = true
	s.needed[lib] = true
	s.pkgDirs[tool] = &api.PkgDirs{SourceDir: toolDir, BuildDir: canonicalTempDir(t)}
	s.pkgDirs[lib] = &api.PkgDirs{SourceDir: libDir, BuildDir: canonicalTempDir(t)}
	upstream := filepath.Join(canonicalTempDir(t), "third_party", "lib", "src")
	libNode.Pkg.SetSrcDir(upstream)

	if got := s.computeDepSrcDir(tool, "//lib"); got != upstream {
		t.Fatalf("intra-container package ref = %q, want %q", got, upstream)
	}
	if got := s.computeDepSrcDir(tool, lib+":utils"); got != upstream {
		t.Fatalf("full package ref = %q, want %q", got, upstream)
	}
	if got := s.computeDepSrcDir(tool, "//missing"); got != "" {
		t.Fatalf("missing package = %q, want empty", got)
	}
}

func TestApplyBuildContextConfigResolvesIntraContainerImports(t *testing.T) {
	s := sessionFixture(t)
	containerID := "native/root"
	tool := "native/root/tool"
	lib := "native/root/lib"
	toolNode := addNativeMemberNode(s, containerID, "tool", filepath.Join(canonicalTempDir(t), "tool"))
	libPkg := api.NewPackage().SetName(lib)
	cfgCtx := api.NewConfigContext(lib)
	cfgCtx.Option("feature").SetType(api.OptionBool).SetDefault(true)
	api.BindSetOptions(&libPkg.ConfigAccessor, cfgCtx.GetOptions())
	api.BindSetCfgVals(&libPkg.ConfigAccessor, map[string]any{"feature": true})
	libSrc := buildscript.NewSource(lib, "build.go", canonicalTempDir(t), api.SourceRemote)
	libSrc.Container = containerID
	libSrc.RelPath = "lib"
	s.ctx.DepGraph.Packages[lib] = resolver.NewPackageNode(lib, containerID, "lib", resolver.SourceKindNative, libSrc, libPkg)
	needed := map[string]bool{tool: true, lib: true}

	for _, defines := range []bool{false, true} {
		t.Run(map[bool]string{false: "header-only", true: "defines"}[defines], func(t *testing.T) {
			node := resolver.NewPackageNode(tool, containerID, "tool", resolver.SourceKindNative, toolNode.Source, api.NewPackage().SetName(tool))
			buildCtx := api.NewBuildContext(tool, nil)
			api.BindSetPackage(buildCtx, node.Pkg)
			target := buildCtx.Target("app").SetKind(api.TargetStatic)
			buildCtx.GenerateConfigHeader()
			if defines {
				buildCtx.GenerateConfigDefines()
			}
			buildCtx.ImportConfig("//lib")
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

func TestWriteLockfileLocksContainerNotNonGitMember(t *testing.T) {
	s := sessionFixture(t)
	containerID, memberID := "native/root", "native/root/member"
	delete(s.ctx.DepGraph.Packages, "//app")
	delete(s.ctx.DepGraph.Packages, "//dep")
	s.ctx.DepGraph.Order = nil
	s.ctx.DepGraph.Containers[containerID] = &resolver.ContainerNode{
		ID:       containerID,
		Repo:     "native",
		Kind:     resolver.SourceKindNative,
		URL:      "https://example.invalid/root.git",
		Versions: map[string]string{"1.0.0": "v1.0.0"},
		Version:  "1.0.0",
		Commit:   "abc123",
	}
	addNativeMemberNode(s, containerID, "member", canonicalTempDir(t))
	s.needed = map[string]bool{memberID: true}
	s.containerVersions[containerID] = "1.0.0"
	s.containerCommits[containerID] = "abc123"
	s.ctx.LockPath = filepath.Join(canonicalTempDir(t), "vmake.lock")
	s.ctx.Lock = lockfile.New()
	s.ctx.Lock.SetSource(memberID, &lockfile.LockedSource{Version: "1.0.0", Commit: "stale"})

	if err := s.writeLockfile(); err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Load(s.ctx.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := locked.GetContainer(containerID)
	if !ok || entry.Version != "1.0.0" || entry.Commit != "abc123" || entry.Source != "native" {
		t.Fatalf("container lock = %+v", entry)
	}
	if _, ok := locked.GetSource(memberID); ok {
		t.Fatal("non-SetGit container member must not be locked in sources")
	}
}

func TestPreparePackagesNestedNativeMember(t *testing.T) {
	s := sessionFixture(t)
	containerID, memberID, member := "native/root", "native/root/a/b", "a/b"
	tree := remoteTreeDir(s.ctx, containerID)
	packageRoot := filepath.Join(tree, "src", filepath.FromSlash(member))
	if err := writePipelineFile(packageRoot, "build.go"); err != nil {
		t.Fatal(err)
	}
	delete(s.ctx.DepGraph.Packages, "//app")
	delete(s.ctx.DepGraph.Packages, "//dep")
	s.ctx.DepGraph.Order = nil
	s.ctx.DepGraph.Containers[containerID] = &resolver.ContainerNode{
		ID:       containerID,
		Repo:     "native",
		Kind:     resolver.SourceKindNative,
		Dir:      filepath.Join(tree, "src"),
		URL:      "unused",
		Versions: map[string]string{"1.0.0": "v1.0.0"},
		Version:  "1.0.0",
		Commit:   "resolved-commit",
	}
	addNativeMemberNode(s, containerID, member, packageRoot)
	s.ctx.Pins = map[string]string{containerID: "1.0.0"}
	s.needed = map[string]bool{memberID: true}
	s.allPkgOptions[memberID] = nil
	s.containerVersions[containerID] = "1.0.0"
	s.containerCommits[containerID] = "resolved-commit"
	s.containerTrees[containerID] = tree

	if err := s.preparePackages(&sourceDownloadLog{}); err != nil {
		t.Fatal(err)
	}
	if got := s.containerCommits[containerID]; got != "resolved-commit" {
		t.Fatalf("container commit = %q, want resolved-commit", got)
	}
	if got := s.containerTrees[containerID]; got != tree {
		t.Fatalf("container tree = %q, want %q", got, tree)
	}
	dirs := s.pkgDirs[memberID]
	if dirs == nil || dirs.SourceDir != packageRoot {
		t.Fatalf("member dirs = %+v, want source %s", dirs, packageRoot)
	}
	if !strings.HasPrefix(dirs.BuildDir, filepath.Join(tree, "out")+string(filepath.Separator)) {
		t.Fatalf("member BuildDir = %s, want under %s", dirs.BuildDir, filepath.Join(tree, "out"))
	}
}

func TestRemoteOwnerAndMemberPathNested(t *testing.T) {
	r := resolver.NewResolver(nil, canonicalTempDir(t))
	src := buildscript.NewSource("native/root/a/b", "build.go", canonicalTempDir(t), api.SourceRemote)
	src.Container = "native/root"
	src.RelPath = "a/b"
	r.Graph().Packages["native/root/a/b"] = resolver.NewPackageNode("native/root/a/b", "native/root", "a/b", resolver.SourceKindNative, src, api.NewPackage())
	rootSrc := buildscript.NewSource("native/root", "build.go", canonicalTempDir(t), api.SourceRemote)
	rootSrc.Container = "native/root"
	r.Graph().Packages["native/root"] = resolver.NewPackageNode("native/root", "native/root", "", resolver.SourceKindNative, rootSrc, api.NewPackage())
	ctx := &RuntimeContext{Resolver: r, DepGraph: r.Graph(), Paths: &Paths{DepsDir: canonicalTempDir(t)}}

	if got := remoteOwnerName(ctx, "native/root/a/b"); got != "native/root" {
		t.Fatalf("owner = %q, want native/root", got)
	}
	if got := ctx.packageSource("native/root/a/b", r.Graph().Packages["native/root/a/b"]).Member(); got != "a/b" {
		t.Fatalf("member = %q, want a/b", got)
	}
	if got := ctx.packageSource("native/root", r.Graph().Packages["native/root"]).Member(); got != "" {
		t.Fatalf("root member = %q, want empty", got)
	}
}

func TestNeededContainersCollectsContainerWithoutRootPackage(t *testing.T) {
	s := sessionFixture(t)
	containerID, memberID := "native/root", "native/root/member"
	addNativeMemberNode(s, containerID, "member", canonicalTempDir(t))
	s.ctx.DepGraph.Roots = []string{"//app"}
	s.ctx.DepGraph.Packages["//app"].Deps = []string{memberID}

	needed, err := resolveNeeded(s.ctx, true, true)
	if err != nil {
		t.Fatalf("resolveNeeded failed for member without root package: %v", err)
	}
	if !needed[memberID] {
		t.Fatal("member was not marked needed")
	}
	containers := NeededContainers(s.ctx.DepGraph, needed)
	if !containers[containerID] || containers[api.LocalContainerID] {
		t.Fatalf("NeededContainers = %+v, want only external container %s", containers, containerID)
	}
}

func TestSessionBuildRequestResolvesIntraContainerRef(t *testing.T) {
	s, tree, memberA := nativeMemberFixture(t, "member_a", false)
	containerID := "native/root"
	memberB := containerID + "/member_b"
	dirB := filepath.Join(tree, "src", "member_b")
	if err := writePipelineFile(dirB, "build.go"); err != nil {
		t.Fatal(err)
	}
	nodeB := addNativeMemberNode(s, containerID, "member_b", dirB)
	s.ctx.DepGraph.Order = []string{memberA, memberB, "//app"}
	s.needed[memberB] = true
	s.computeDirsAndOptions(t)

	builds := 0
	nodeB.Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("b").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error {
			builds++
			return nil
		})
	})
	s.ctx.DepGraph.Packages[memberA].Pkg.OnBuild(func(ctx *api.BuildContext) {
		ctx.BuildSubGraph("//member_b")
		ctx.Target("a").SetKind(api.TargetVoid)
	})

	if err := s.executeOnBuild(); err != nil {
		t.Fatal(err)
	}
	if builds != 1 {
		t.Fatalf("member_b built %d times, want 1", builds)
	}
}

func TestSessionMissingBuildPackageFails(t *testing.T) {
	s := sessionFixture(t)
	containerID, tool := "native/root", "native/root/tool"
	addNativeMemberNode(s, containerID, "tool", filepath.Join(canonicalTempDir(t), "tool"))
	addNativeMemberNode(s, containerID, "lib", filepath.Join(canonicalTempDir(t), "lib"))
	s.needed[tool] = true

	err := s.buildSubGraph(tool, "//lib")
	if err == nil || !strings.Contains(err.Error(), "not found in required packages") {
		t.Fatalf("missing subgraph error = %v, want not found in required packages", err)
	}
}

func writePipelineFile(dir, name string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), []byte("package main\n"), 0644)
}
