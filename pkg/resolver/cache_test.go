package resolver

import (
	"testing"
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

func TestSetGlobalSourcesDir(t *testing.T) {
	r := NewResolver(nil, "/tmp/deps")
	r.SetGlobalSourcesDir("/global/sources")
}

func TestResolveDeferredEmpty(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	if err := r.ResolveDeferred(); err != nil {
		t.Errorf("ResolveDeferred with empty graph should succeed: %v", err)
	}
}

func TestResolveDeferredOnlyNonDeferred(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	r.graph.Packages["a"] = &PackageNode{ID: "a", Deferred: false, Deps: []string{}}
	r.graph.Packages["b"] = &PackageNode{ID: "b", Deferred: false, Deps: []string{"a"}}

	if err := r.ResolveDeferred(); err != nil {
		t.Errorf("ResolveDeferred with no deferred nodes should succeed: %v", err)
	}
}
