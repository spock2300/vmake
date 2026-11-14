package pipeline

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/lockfile"
)

func TestProjectLockDecisions(t *testing.T) {
	existing := lockfile.New()
	existing.Set("keep/pkg", &lockfile.LockedPkg{Version: "1.0.0", Commit: "keep", Source: "native"})
	existing.Set("stale/pkg", &lockfile.LockedPkg{Version: "9.9.9", Commit: "stale", Source: "native"})

	decisions := []lockDecision{
		{Name: "keep/pkg", Keep: true},
		{Name: "fresh/pkg", Version: "2.0.0", Commit: "fresh", Source: "git"},
		{Name: "skipped/pkg", Skip: true},
		{Name: "empty/pkg"},
		{Name: "registry/pkg", Version: "3.0.0", Commit: "reg", Source: "registry", WrapperCommit: "wrapper"},
		{Name: "missing-keep/pkg", Keep: true},
	}
	got := projectLock(decisions, existing)
	if len(got.Packages) != 3 {
		t.Fatalf("projected packages = %d, want 3: %+v", len(got.Packages), got.Packages)
	}
	if entry, ok := got.Get("keep/pkg"); !ok || entry.Commit != "keep" {
		t.Fatalf("keep entry = %+v, %v", entry, ok)
	}
	if entry, ok := got.Get("fresh/pkg"); !ok || entry.Source != "git" || entry.Commit != "fresh" {
		t.Fatalf("fresh entry = %+v, %v", entry, ok)
	}
	if entry, ok := got.Get("registry/pkg"); !ok || entry.WrapperCommit != "wrapper" {
		t.Fatalf("registry entry = %+v, %v", entry, ok)
	}
	if _, ok := got.Get("stale/pkg"); ok {
		t.Fatal("stale entry survived projection")
	}
	if _, ok := got.Get("skipped/pkg"); ok {
		t.Fatal("skipped package was projected")
	}
	if _, ok := got.Get("empty/pkg"); ok {
		t.Fatal("empty package was projected")
	}
}

func TestValidateLockedProjection(t *testing.T) {
	existing := lockfile.New()
	existing.Set("pkg", &lockfile.LockedPkg{Version: "1.0.0", Commit: "old", Source: "native"})
	equal := []lockDecision{{Name: "pkg", Version: "1.0.0", Commit: "old", Source: "native"}}
	if err := validateLockedProjection(equal, existing); err != nil {
		t.Fatalf("equal projection rejected: %v", err)
	}

	changed := []lockDecision{{Name: "pkg", Version: "2.0.0", Commit: "new", Source: "native"}}
	err := validateLockedProjection(changed, existing)
	if err == nil || !strings.Contains(err.Error(), "--locked: vmake.lock is out of date (pkg (updated))") {
		t.Fatalf("changed projection error = %v", err)
	}

	if err := validateLockedProjection(nil, nil); err != nil {
		t.Fatalf("empty projection rejected: %v", err)
	}
	if err := validateLockedProjection(nil, existing); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("missing lock not detected: %v", err)
	}
}
