package build

import "testing"

func TestParseDependencyRef(t *testing.T) {
	tests := []struct {
		in        string
		pkg       string
		target    string
		hasTarget bool
		path      bool
	}{
		{in: "target", pkg: "target"},
		{in: "pkg:target", pkg: "pkg", target: "target", hasTarget: true},
		{in: "pkg/sub:target", pkg: "pkg/sub", target: "target", hasTarget: true, path: true},
		{in: "pkg/sub", pkg: "pkg/sub", path: true},
		{in: "pkg:*", pkg: "pkg", target: "*", hasTarget: true},
		{in: "pkg/sub:*", pkg: "pkg/sub", target: "*", hasTarget: true, path: true},
		{in: ":target", pkg: "", target: "target", hasTarget: true},
		{in: "", pkg: ""},
		{in: "pkg:", pkg: "pkg", hasTarget: true},
		{in: "a:b:c", pkg: "a", target: "b:c", hasTarget: true},
		{in: " pkg:target ", pkg: " pkg", target: "target ", hasTarget: true},
	}
	for _, tc := range tests {
		got := ParseDependencyRef(tc.in)
		if got.Package != tc.pkg || got.Target != tc.target || got.HasTarget != tc.hasTarget {
			t.Errorf("parseDependencyRef(%q) = %+v", tc.in, got)
		}
		if got.IsPackagePath() != tc.path {
			t.Errorf("parseDependencyRef(%q).packagePath = %v, want %v", tc.in, got.IsPackagePath(), tc.path)
		}
	}
}
