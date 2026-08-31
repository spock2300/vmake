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
	if r.GetOrder() != nil {
		t.Error("Order should be nil/empty initially")
	}
	if r.SubParents() == nil {
		t.Error("SubParents should not be nil")
	}
}

func TestLockfileEntryAndConfigPins(t *testing.T) {
	r := NewResolver(nil, "/tmp/deps")
	l := lockfile.New()
	l.Set("repo/pkg", &lockfile.LockedPkg{Version: "1.2.3", Commit: "abc"})
	r.SetLockfile(l, false)

	if got, ok := r.lockfileEntry("repo/pkg"); !ok || got.Version != "1.2.3" || got.Commit != "abc" {
		t.Errorf("lockfileEntry = %+v, %v", got, ok)
	}
	if _, ok := r.lockfileEntry("other/pkg"); ok {
		t.Error("lockfileEntry for missing key should return ok=false")
	}

	if v, c, ok := r.pinnedVersion("repo/pkg"); !ok || v != "1.2.3" || c != "abc" {
		t.Errorf("pinnedVersion from lock = %q, %q, %v", v, c, ok)
	}

	r.SetConfigPins(map[string]string{"repo/pkg": "2.0.0"})
	if v, c, ok := r.pinnedVersion("repo/pkg"); !ok || v != "2.0.0" || c != "" {
		t.Errorf("pinnedVersion with config pin = %q, %q, %v", v, c, ok)
	}

	r.SetLockfile(l, true)
	if _, _, ok := r.pinnedVersion("repo/pkg"); !ok {
		t.Error("config pin should still apply when lock is ignored")
	}
	r.SetConfigPins(nil)
	if _, _, ok := r.pinnedVersion("repo/pkg"); ok {
		t.Error("pinnedVersion should return ok=false when lock ignored and no config pin")
	}
}
