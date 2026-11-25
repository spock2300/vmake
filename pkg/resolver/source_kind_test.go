package resolver

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

func TestSourceKindNamesAndCompatibilitySpelling(t *testing.T) {
	local := NewPackageNode("//app", "//", "app", SourceKindLocal, &buildscript.Source{Origin: api.SourceLocal}, nil)
	native := NewPackageNode("hl/sdk/sub", "hl/sdk", "sub", SourceKindNative, &buildscript.Source{Origin: api.SourceRemote}, nil)
	registry := NewPackageNode("official/zlib", "official", "zlib", SourceKindRegistry, &buildscript.Source{Origin: api.SourceRemote}, nil)

	cases := []struct {
		node *PackageNode
		kind SourceKind
	}{
		{local, SourceKindLocal},
		{native, SourceKindNative},
		{registry, SourceKindRegistry},
	}
	for _, tc := range cases {
		if got := tc.node.SourceKind(); got != tc.kind {
			t.Errorf("%s: kind = %q, want %q", tc.node.ID, got, tc.kind)
		}
		if got := tc.node.SourceKind().String(); got != string(tc.kind) {
			t.Errorf("%s: String = %q, want %q", tc.node.ID, got, tc.kind)
		}
	}
}
