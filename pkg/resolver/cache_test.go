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
