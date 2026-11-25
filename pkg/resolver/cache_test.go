package resolver

import (
	"testing"

	"github.com/spock2300/vmake/pkg/lockfile"
)

func TestNewResolverInitializes(t *testing.T) {
	r := NewResolver(nil, "/tmp/deps")
	if r.Graph() == nil {
		t.Error("Graph() should not be nil")
	}
	if r.Graph().Packages == nil {
		t.Error("Packages map should not be nil")
	}
	if r.Graph().Containers == nil {
		t.Error("Containers map should not be nil")
	}
	if r.GetOrder() != nil {
		t.Error("Order should be nil/empty initially")
	}
}

func TestLockfileEntryAndConfigPins(t *testing.T) {
	r := NewResolver(nil, "/tmp/deps")
	l := lockfile.New()
	l.SetContainer("repo/pkg", &lockfile.LockedContainer{Version: "1.2.3", Commit: "abc", Source: "native"})
	r.SetLockfile(l, false)

	if got, ok := r.lockQuery().Containers().LockEntry("repo/pkg"); !ok || got.Version != "1.2.3" || got.Commit != "abc" {
		t.Errorf("LockEntry = %+v, %v", got, ok)
	}
	if _, ok := r.lockQuery().Containers().LockEntry("other/pkg"); ok {
		t.Error("LockEntry for missing key should return ok=false")
	}

	if v, c, ok := r.pinnedContainerVersion("repo/pkg"); !ok || v != "1.2.3" || c != "abc" {
		t.Errorf("pinnedContainerVersion from lock = %q, %q, %v", v, c, ok)
	}

	r.SetConfigPins(map[string]string{"repo/pkg": "2.0.0"})
	if v, c, ok := r.pinnedContainerVersion("repo/pkg"); !ok || v != "2.0.0" || c != "" {
		t.Errorf("pinnedContainerVersion with config pin = %q, %q, %v", v, c, ok)
	}

	r.SetLockfile(l, true)
	if _, _, ok := r.pinnedContainerVersion("repo/pkg"); !ok {
		t.Error("config pin should still apply when lock is ignored")
	}
	r.SetConfigPins(nil)
	if _, _, ok := r.pinnedContainerVersion("repo/pkg"); ok {
		t.Error("pinnedContainerVersion should return ok=false when lock ignored and no config pin")
	}
}

func TestLockPolicyFloatAndTargets(t *testing.T) {
	r := NewResolver(nil, "/tmp/deps")
	l := lockfile.New()
	l.SetContainer("repo/a", &lockfile.LockedContainer{Version: "1.0.0", Commit: "111", Source: "native"})
	l.SetContainer("repo/b", &lockfile.LockedContainer{Version: "2.0.0", Commit: "222", Source: "native"})
	l.SetContainer("repo/head", &lockfile.LockedContainer{Version: "", Commit: "333", Source: "native"})
	r.SetLockfile(l, false)

	r.SetLockPolicy(LockPolicy{
		Float:   map[string]bool{"repo/a": true},
		Targets: map[string]string{"repo/b": "2.1.0"},
	})

	if _, ok := r.lockQuery().Containers().LockEntry("repo/a"); ok {
		t.Error("floated container should not return lockfile entry")
	}
	if _, ok := r.lockQuery().Containers().LockEntry("repo/b"); ok {
		t.Error("targeted container should not return old lockfile entry")
	}
	if v, c, ok := r.pinnedContainerVersion("repo/b"); !ok || v != "2.1.0" || c != "" {
		t.Errorf("targeted container pinnedContainerVersion = %q, %q, %v; want 2.1.0, '', true", v, c, ok)
	}
	if v, c, ok := r.pinnedContainerVersion("repo/head"); !ok || v != "" || c != "333" {
		t.Errorf("commit-locked container pinnedContainerVersion = %q, %q, %v; want '', 333, true", v, c, ok)
	}

	r.SetConfigPins(map[string]string{
		"repo/a": "1.0.0",
		"repo/b": "2.0.0",
	})
	if _, _, ok := r.pinnedContainerVersion("repo/a"); ok {
		t.Error("Float should override configPins")
	}
	if v, c, ok := r.pinnedContainerVersion("repo/b"); !ok || v != "2.1.0" || c != "" {
		t.Errorf("Targets should override configPins: got %q, %q, %v", v, c, ok)
	}

	r.SetLockPolicy(LockPolicy{
		Targets: map[string]string{"repo/b": "2.0.0"},
	})
	if v, c, ok := r.pinnedContainerVersion("repo/b"); !ok || v != "2.0.0" || c != "" {
		t.Errorf("explicit target matching lock version should not reuse locked commit: got %q, %q, %v", v, c, ok)
	}
	if v, c, ok := r.pinnedContainerVersion("repo/a"); !ok || v != "1.0.0" || c != "111" {
		t.Errorf("configPin matching lock version should reuse locked commit: got %q, %q, %v", v, c, ok)
	}
}
