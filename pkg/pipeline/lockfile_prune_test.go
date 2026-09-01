package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestWriteLockfilePrunesStaleEntries(t *testing.T) {
	tmp := t.TempDir()

	r := resolver.NewResolver(nil, tmp)
	node := &resolver.PackageNode{
		ID:     "testnative/foo",
		Deps:   []string{},
		Native: &resolver.NativePackageInfo{Selected: "1.9.0"},
	}
	r.Graph().Packages["testnative/foo"] = node
	r.Graph().Order = []string{"testnative/foo"}

	old := lockfile.New()
	old.Set("gone/pkg", &lockfile.LockedPkg{Version: "0.1.0", Commit: "deadbeef", Source: "native"})
	old.Set("testnative/foo", &lockfile.LockedPkg{Version: "1.2.0", Commit: "aaaa", Source: "native"})

	lockPath := filepath.Join(tmp, "vmake.lock")
	if err := old.Save(lockPath); err != nil {
		t.Fatal(err)
	}

	ctx := &RuntimeContext{
		Resolver: r,
		DepGraph: r.Graph(),
		Lock:     old,
		LockPath: lockPath,
		Paths:    &Paths{ReposDir: tmp},
	}
	s := newBuildPhaseState(ctx, BuildOptions{})
	s.remote = &remoteVersionState{
		entries: map[string]*config.EntryConfig{"testnative/foo": {Version: "1.9.0"}},
		commits: map[string]string{"testnative/foo": "bbbb"},
	}

	if err := s.writeLockfile(); err != nil {
		t.Fatalf("writeLockfile: %v", err)
	}

	got, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Get("gone/pkg"); ok {
		t.Error("stale entry gone/pkg should be pruned from lock")
	}
	kept, ok := got.Get("testnative/foo")
	if !ok {
		t.Fatal("active entry testnative/foo should be kept")
	}
	if kept.Version != "1.9.0" || kept.Commit != "bbbb" || kept.Source != "native" {
		t.Errorf("kept entry = %+v, want version 1.9.0 commit bbbb source native", kept)
	}
	if kept.WrapperCommit != "" {
		t.Errorf("native entry should not carry WrapperCommit, got %q", kept.WrapperCommit)
	}
}
