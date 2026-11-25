package resolver

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

func TestSourceKindNamesAndCompatibilitySpelling(t *testing.T) {
	local := NewPackageNode("app", &buildscript.Source{Origin: api.SourceLocal}, nil)
	native := NewPackageNode("lib", &buildscript.Source{Origin: api.SourceRemote}, nil).WithNative("u", nil, "")
	registry := NewPackageNode("zlib", &buildscript.Source{Origin: api.SourceRemote}, nil)

	cases := []struct {
		node *PackageNode
		kind SourceKind
		git  string
	}{
		{local, SourceKindLocal, "git"},
		{native, SourceKindNative, "native"},
		{registry, SourceKindRegistry, "registry"},
	}
	for _, tc := range cases {
		if got := tc.node.SourceKind(); got != tc.kind {
			t.Errorf("%s: kind = %q, want %q", tc.node.ID, got, tc.kind)
		}
		if got := tc.node.SourceKind().String(); got != string(tc.kind) {
			t.Errorf("%s: String = %q, want %q", tc.node.ID, got, tc.kind)
		}
		if got := tc.node.SourceKind().GitCompatName(); got != tc.git {
			t.Errorf("%s: GitCompatName = %q, want %q", tc.node.ID, got, tc.git)
		}
	}
}
