package pipeline

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestProjectLockDecisions(t *testing.T) {
	existing := lockfile.New()
	existing.SetContainer("keep/mod", &lockfile.LockedContainer{Version: "1.0.0", Commit: "keep", Source: "native"})
	existing.SetContainer("stale/mod", &lockfile.LockedContainer{Version: "9.9.9", Commit: "stale", Source: "native"})
	existing.SetSource("stale/src", &lockfile.LockedSource{Version: "1.0.0", Commit: "stale"})

	decisions := []lockDecision{
		{Section: resolver.LockSectionContainer, Name: "keep/mod", Keep: true},
		{Section: resolver.LockSectionContainer, Name: "official", Commit: "reg-head", Source: "registry"},
		{Section: resolver.LockSectionSource, Name: "fresh/pkg", Version: "2.0.0", Commit: "fresh", URL: "https://example.invalid/fresh.git"},
		{Section: resolver.LockSectionSource, Name: "skipped/pkg", Skip: true},
		{Section: resolver.LockSectionSource, Name: "empty/pkg"},
		{Section: resolver.LockSectionContainer, Name: "missing-keep/mod", Keep: true},
	}
	got := projectLock(decisions, existing)
	if len(got.Containers) != 2 || len(got.Sources) != 1 {
		t.Fatalf("projected = containers:%d sources:%d, want 2/1: %+v", len(got.Containers), len(got.Sources), got)
	}
	if entry, ok := got.GetContainer("keep/mod"); !ok || entry.Commit != "keep" {
		t.Fatalf("keep entry = %+v, %v", entry, ok)
	}
	if entry, ok := got.GetContainer("official"); !ok || entry.Source != "registry" || entry.Commit != "reg-head" {
		t.Fatalf("registry container entry = %+v, %v", entry, ok)
	}
	if entry, ok := got.GetSource("fresh/pkg"); !ok || entry.Commit != "fresh" || entry.URL != "https://example.invalid/fresh.git" {
		t.Fatalf("fresh source entry = %+v, %v", entry, ok)
	}
	if _, ok := got.GetContainer("stale/mod"); ok {
		t.Fatal("stale container survived projection")
	}
	if _, ok := got.GetSource("stale/src"); ok {
		t.Fatal("stale source survived projection")
	}
}

func TestValidateLockedProjection(t *testing.T) {
	existing := lockfile.New()
	existing.SetContainer("repo/mod", &lockfile.LockedContainer{Version: "1.0.0", Commit: "old", Source: "native"})
	equal := []lockDecision{{Section: resolver.LockSectionContainer, Name: "repo/mod", Version: "1.0.0", Commit: "old", Source: "native"}}
	if err := validateLockedProjection(equal, existing); err != nil {
		t.Fatalf("equal projection rejected: %v", err)
	}

	changed := []lockDecision{{Section: resolver.LockSectionContainer, Name: "repo/mod", Version: "2.0.0", Commit: "new", Source: "native"}}
	err := validateLockedProjection(changed, existing)
	if err == nil || !strings.Contains(err.Error(), "--locked: vmake.lock is out of date (repo/mod (updated))") {
		t.Fatalf("changed projection error = %v", err)
	}

	if err := validateLockedProjection(nil, nil); err != nil {
		t.Fatalf("empty projection rejected: %v", err)
	}
	if err := validateLockedProjection(nil, existing); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("missing lock not detected: %v", err)
	}
}

func TestLockDecisionsExcludesUnneededContainers(t *testing.T) {
	graph := &resolver.Graph{
		Containers: map[string]*resolver.ContainerNode{
			"//":             {ID: "//", Kind: resolver.SourceKindLocal},
			"myorg/used":     {ID: "myorg/used", Kind: resolver.SourceKindNative, Version: "1.0.0", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			"myorg/disabled": {ID: "myorg/disabled", Kind: resolver.SourceKindNative, Version: "2.0.0", Commit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		},
		Packages: map[string]*resolver.PackageNode{
			"//":             {ID: "//", Container: "//", Kind: resolver.SourceKindLocal, Pkg: api.NewPackage(), Source: &buildscript.Source{Name: "//", Origin: api.SourceLocal}},
			"myorg/used":     {ID: "myorg/used", Container: "myorg/used", Kind: resolver.SourceKindNative, Pkg: api.NewPackage(), Source: &buildscript.Source{Name: "myorg/used", Origin: api.SourceRemote}},
			"myorg/disabled": {ID: "myorg/disabled", Container: "myorg/disabled", Kind: resolver.SourceKindNative, Pkg: api.NewPackage(), Source: &buildscript.Source{Name: "myorg/disabled", Origin: api.SourceRemote}},
		},
		Order: []string{"myorg/used", "myorg/disabled", "//"},
	}
	s := &sourceStage{
		rc:   &runContext{ctx: &RuntimeContext{DepGraph: graph}},
		conf: &configStage{needed: map[string]bool{"//": true, "myorg/used": true}},
		containerVersions: map[string]string{
			"myorg/used": "1.0.0",
		},
		containerCommits: map[string]string{
			"myorg/used": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
	decisions, err := s.lockDecisions()
	if err != nil {
		t.Fatalf("lockDecisions() error = %v", err)
	}
	got := projectLock(decisions, nil)
	if _, ok := got.GetContainer("myorg/used"); !ok {
		t.Fatal("needed container myorg/used missing from lock projection")
	}
	if _, ok := got.GetContainer("myorg/disabled"); ok {
		t.Fatal("unneeded container myorg/disabled leaked into lock projection")
	}
}
