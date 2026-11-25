package pipeline

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/resolver"
)

func lockWithContainer(name, version, commit, source string) *lockfile.Lock {
	l := lockfile.New()
	l.SetContainer(name, &lockfile.LockedContainer{Version: version, Commit: commit, Source: source})
	return l
}

func lockWithSource(name, version, commit, url string) *lockfile.Lock {
	l := lockfile.New()
	l.SetSource(name, &lockfile.LockedSource{Version: version, Commit: commit, URL: url})
	return l
}

func lockProjectionStage(t *testing.T, containers []*resolver.ContainerNode, nodes []*resolver.PackageNode, old *lockfile.Lock, policy resolver.LockPolicy, links map[string]string) *sourceStage {
	t.Helper()
	r := resolver.NewResolver(nil, t.TempDir())
	for _, c := range containers {
		r.Graph().Containers[c.ID] = c
	}
	needed := make(map[string]bool)
	for _, node := range nodes {
		r.Graph().Packages[node.ID] = node
		r.Graph().Order = append(r.Graph().Order, node.ID)
		needed[node.ID] = true
	}
	r.SetLockfile(old, policy.IgnoreAll)
	r.SetLockPolicy(policy)
	ctx := &RuntimeContext{
		Resolver:    r,
		DepGraph:    r.Graph(),
		Lock:        old,
		LockPolicy:  policy,
		SourceLinks: links,
		Paths:       &Paths{DepsDir: t.TempDir(), ReposDir: t.TempDir()},
	}
	s := newSourceStage(&runContext{ctx: ctx, opts: BuildOptions{}}, &configStage{needed: needed})
	return s
}

func TestBuildLockfileProjectionMatrix(t *testing.T) {
	localGitNode := func(id string) *resolver.PackageNode {
		src := buildscript.NewSource(id, "/src/build.go", "/src", api.SourceLocal)
		pkg := api.NewPackage().SetName(id)
		pkg.SetGit("https://example.invalid/local.git")
		return resolver.NewPackageNode(id, "//", "pkg", resolver.SourceKindLocal, src, pkg)
	}
	nativeOwnGitNode := func(id, container string) *resolver.PackageNode {
		src := buildscript.NewSource(id, "/sdk/lvgl/build.go", "/sdk/lvgl", api.SourceRemote)
		pkg := api.NewPackage().SetName(id)
		pkg.SetGit("https://example.invalid/lvgl.git")
		return resolver.NewPackageNode(id, container, "lvgl", resolver.SourceKindNative, src, pkg)
	}

	t.Run("native container uses materialized state", func(t *testing.T) {
		old := lockfile.New()
		old.SetContainer("gone/mod", &lockfile.LockedContainer{Version: "9.9.9", Commit: "deadbeef", Source: "native"})
		s := lockProjectionStage(t, []*resolver.ContainerNode{
			{ID: "repo/native", Repo: "repo", Kind: resolver.SourceKindNative},
		}, nil, old, resolver.LockPolicy{}, nil)
		s.containerVersions["repo/native"] = "1.2.3"
		s.containerCommits["repo/native"] = "commit-native"
		got, err := s.buildLockfile()
		if err != nil {
			t.Fatal(err)
		}
		c, ok := got.GetContainer("repo/native")
		if !ok || c.Version != "1.2.3" || c.Commit != "commit-native" || c.Source != "native" {
			t.Fatalf("container = %+v, %v", c, ok)
		}
		if _, stale := got.GetContainer("gone/mod"); stale {
			t.Fatal("stale container should be pruned")
		}
	})

	t.Run("native member with own SetGit writes source entry orthogonally", func(t *testing.T) {
		s := lockProjectionStage(t, []*resolver.ContainerNode{
			{ID: "hl/sdk", Repo: "hl", Kind: resolver.SourceKindNative, Version: "0.3.0", Commit: "sdk-commit"},
		}, []*resolver.PackageNode{
			nativeOwnGitNode("hl/sdk/lvgl", "hl/sdk"),
		}, lockfile.New(), resolver.LockPolicy{}, nil)
		s.containerVersions["hl/sdk"] = "0.3.0"
		s.containerCommits["hl/sdk"] = "sdk-commit"
		s.sourceVersions["hl/sdk/lvgl"] = "9.2.2"
		s.sourceCommits["hl/sdk/lvgl"] = "lvgl-commit"
		got, err := s.buildLockfile()
		if err != nil {
			t.Fatal(err)
		}
		if c, ok := got.GetContainer("hl/sdk"); !ok || c.Commit != "sdk-commit" {
			t.Fatalf("container = %+v, %v", c, ok)
		}
		if src, ok := got.GetSource("hl/sdk/lvgl"); !ok || src.Version != "9.2.2" || src.Commit != "lvgl-commit" {
			t.Fatalf("own-Git source = %+v, %v", src, ok)
		}
	})

	t.Run("local SetGit writes source entry and retains on unchanged", func(t *testing.T) {
		s := lockProjectionStage(t, nil, []*resolver.PackageNode{localGitNode("//pkg")},
			lockWithSource("//pkg", "0.1.0", "old-commit", "https://example.invalid/local.git"),
			resolver.LockPolicy{}, nil)
		got, err := s.buildLockfile()
		if err != nil {
			t.Fatal(err)
		}
		if src, ok := got.GetSource("//pkg"); !ok || src.Commit != "old-commit" {
			t.Fatalf("retained local SetGit source = %+v, %v", src, ok)
		}
	})

	t.Run("linked container keeps old container entry", func(t *testing.T) {
		s := lockProjectionStage(t, []*resolver.ContainerNode{
			{ID: "link/sdk", Repo: "link", Kind: resolver.SourceKindNative, Link: &buildscript.SourceLink{Path: "/work/sdk", Native: true, LocalSource: true}},
		}, nil, lockWithContainer("link/sdk", "1.0.0", "link-commit", "native"), resolver.LockPolicy{}, map[string]string{"link/sdk": "/work/sdk"})
		got, err := s.buildLockfile()
		if err != nil {
			t.Fatal(err)
		}
		if c, ok := got.GetContainer("link/sdk"); !ok || c.Commit != "link-commit" {
			t.Fatalf("linked container entry = %+v, %v", c, ok)
		}
	})

	t.Run("float drops stale container entry", func(t *testing.T) {
		s := lockProjectionStage(t, []*resolver.ContainerNode{
			{ID: "repo/native", Repo: "repo", Kind: resolver.SourceKindNative},
		}, nil, lockWithContainer("repo/native", "1.0.0", "old-commit", "native"),
			resolver.LockPolicy{Float: map[string]bool{"repo/native": true}}, nil)
		got, err := s.buildLockfile()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got.GetContainer("repo/native"); ok {
			t.Fatal("floated container without new resolution should be absent")
		}
	})
}
