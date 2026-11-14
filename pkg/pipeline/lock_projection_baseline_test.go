package pipeline

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/resolver"
)

func lockProjectionNode(name string, local bool, git bool) *resolver.PackageNode {
	origin := api.SourceRemote
	if local {
		origin = api.SourceLocal
	}
	src := buildscript.NewSource(name, "/src/"+name+"/build.go", "/src/"+name, origin)
	pkg := api.NewPackage().SetName(name)
	if git {
		pkg.SetGit("https://example.invalid/" + name + ".git")
	}
	return resolver.NewPackageNode(name, src, pkg)
}

func nativeLockProjectionNode(name string) *resolver.PackageNode {
	node := lockProjectionNode(name, false, false)
	node.Native = &resolver.NativePackageInfo{GitURL: "https://example.invalid/" + name + ".git"}
	return node
}

func lockProjectionStage(t *testing.T, nodes []*resolver.PackageNode, old *lockfile.Lock, policy resolver.LockPolicy, links map[string]string) *sourceStage {
	t.Helper()
	r := resolver.NewResolver(nil, t.TempDir())
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
	s.remote = &remoteVersionState{versions: map[string]string{}, commits: map[string]string{}}
	return s
}

func TestBuildLockfileProjectionMatrix(t *testing.T) {
	tests := []struct {
		name       string
		nodes      []*resolver.PackageNode
		old        *lockfile.Lock
		policy     resolver.LockPolicy
		links      map[string]string
		versions   map[string]string
		commits    map[string]string
		wantName   string
		wantLocked *lockfile.LockedPkg
		wantAbsent bool
	}{
		{
			name:       "native remote uses materialized state",
			nodes:      []*resolver.PackageNode{nativeLockProjectionNode("repo/native")},
			versions:   map[string]string{"repo/native": "1.2.3"},
			commits:    map[string]string{"repo/native": "commit-native"},
			wantName:   "repo/native",
			wantLocked: &lockfile.LockedPkg{Version: "1.2.3", Commit: "commit-native", Source: "native"},
		},
		{
			name:       "registry remote uses materialized state",
			nodes:      []*resolver.PackageNode{lockProjectionNode("official/foo", false, false)},
			versions:   map[string]string{"official/foo": "2.0.0"},
			commits:    map[string]string{"official/foo": "commit-registry"},
			wantName:   "official/foo",
			wantLocked: &lockfile.LockedPkg{Version: "2.0.0", Commit: "commit-registry", Source: "registry"},
		},
		{
			name:       "local SetGit uses source version and commit",
			nodes:      []*resolver.PackageNode{lockProjectionNode("local/pkg", true, true)},
			versions:   map[string]string{"local/pkg": "0.1.0"},
			commits:    map[string]string{"local/pkg": "commit-local"},
			wantName:   "local/pkg",
			wantLocked: &lockfile.LockedPkg{Version: "0.1.0", Commit: "commit-local", Source: "git"},
		},
		{
			name:       "linked package keeps old entry",
			nodes:      []*resolver.PackageNode{lockProjectionNode("link/pkg", false, false)},
			old:        lockWith("link/pkg", "1.0.0", "link-commit", "native"),
			links:      map[string]string{"link/pkg": "/work/link"},
			wantName:   "link/pkg",
			wantLocked: &lockfile.LockedPkg{Version: "1.0.0", Commit: "link-commit", Source: "native"},
		},
		{
			name:       "local SetGit without new state keeps git lock",
			nodes:      []*resolver.PackageNode{lockProjectionNode("local/pkg", true, true)},
			old:        lockWith("local/pkg", "0.1.0", "old-commit", "git"),
			wantName:   "local/pkg",
			wantLocked: &lockfile.LockedPkg{Version: "0.1.0", Commit: "old-commit", Source: "git"},
		},
		{
			name:       "remote without new state keeps lock",
			nodes:      []*resolver.PackageNode{nativeLockProjectionNode("repo/native")},
			old:        lockWith("repo/native", "1.0.0", "old-commit", "native"),
			wantName:   "repo/native",
			wantLocked: &lockfile.LockedPkg{Version: "1.0.0", Commit: "old-commit", Source: "native"},
		},
		{
			name:       "float drops stale remote entry",
			nodes:      []*resolver.PackageNode{nativeLockProjectionNode("repo/native")},
			old:        lockWith("repo/native", "1.0.0", "old-commit", "native"),
			policy:     resolver.LockPolicy{Float: map[string]bool{"repo/native": true}},
			wantName:   "repo/native",
			wantAbsent: true,
		},
		{
			name: "stale entry is pruned",
			nodes: []*resolver.PackageNode{
				nativeLockProjectionNode("repo/native"),
			},
			old:        lockfile.New(),
			versions:   map[string]string{"repo/native": "1.0.0"},
			commits:    map[string]string{"repo/native": "commit-native"},
			wantName:   "gone/pkg",
			wantAbsent: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			old := tc.old
			if old == nil {
				old = lockfile.New()
			}
			old.Set("gone/pkg", &lockfile.LockedPkg{Version: "9.9.9", Commit: "deadbeef", Source: "native"})
			s := lockProjectionStage(t, tc.nodes, old, tc.policy, tc.links)
			for name, version := range tc.versions {
				s.sourceVersions[name] = version
				s.remote.versions[name] = version
			}
			for name, commit := range tc.commits {
				s.sourceCommits[name] = commit
				s.remote.commits[name] = commit
			}
			updated, err := s.buildLockfile()
			if err != nil {
				t.Fatal(err)
			}
			got, ok := updated.Get(tc.wantName)
			if tc.wantAbsent {
				if ok {
					t.Fatalf("%s should be absent, got %+v", tc.wantName, got)
				}
				return
			}
			if !ok {
				t.Fatalf("%s missing from projected lock", tc.wantName)
			}
			if got.Version != tc.wantLocked.Version || got.Commit != tc.wantLocked.Commit ||
				got.Source != tc.wantLocked.Source || got.WrapperCommit != tc.wantLocked.WrapperCommit {
				t.Fatalf("projected %s = %+v, want %+v", tc.wantName, got, tc.wantLocked)
			}
			if _, stale := updated.Get("gone/pkg"); stale {
				t.Fatal("stale gone/pkg entry should be pruned")
			}
		})
	}
}
