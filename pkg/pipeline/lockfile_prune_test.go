package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestWriteLockfilePrunesStaleEntries(t *testing.T) {
	tmp := t.TempDir()

	r := resolver.NewResolver(nil, tmp)
	r.Graph().Containers["testnative/foo"] = &resolver.ContainerNode{
		ID:      "testnative/foo",
		Repo:    "testnative",
		Kind:    resolver.SourceKindNative,
		Version: "1.9.0",
		Commit:  "bbbb",
	}
	r.Graph().Packages["testnative/foo"] = &resolver.PackageNode{
		ID:        "testnative/foo",
		Container: "testnative/foo",
		Kind:      resolver.SourceKindNative,
		Deps:      []string{},
	}
	r.Graph().Order = []string{"testnative/foo"}

	old := lockfile.New()
	old.SetContainer("gone/pkg", &lockfile.LockedContainer{Version: "0.1.0", Commit: "deadbeef", Source: "native"})
	old.SetContainer("testnative/foo", &lockfile.LockedContainer{Version: "1.2.0", Commit: "aaaa", Source: "native"})
	old.SetSource("gone/src", &lockfile.LockedSource{Version: "1.0.0", Commit: "cccc"})

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
	s.containerVersions["testnative/foo"] = "1.9.0"
	s.containerCommits["testnative/foo"] = "bbbb"

	if err := s.writeLockfile(); err != nil {
		t.Fatalf("writeLockfile: %v", err)
	}

	got, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.GetContainer("gone/pkg"); ok {
		t.Error("stale container gone/pkg should be pruned from lock")
	}
	if _, ok := got.GetSource("gone/src"); ok {
		t.Error("stale source gone/src should be pruned from lock")
	}
	kept, ok := got.GetContainer("testnative/foo")
	if !ok {
		t.Fatal("active container testnative/foo should be kept")
	}
	if kept.Version != "1.9.0" || kept.Commit != "bbbb" || kept.Source != "native" {
		t.Errorf("kept entry = %+v, want version 1.9.0 commit bbbb source native", kept)
	}

	first, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.writeLockfile(); err != nil {
		t.Fatalf("second writeLockfile: %v", err)
	}
	second, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("writeLockfile is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
