package build

import (
	"slices"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestStrictAndAuditAgreeOnNodes(t *testing.T) {
	target := func(name string, kind api.TargetKind, deps ...string) *api.Target {
		target := api.NewTargetRegistry().Target(name)
		target.SetKind(kind)
		target.AddDeps(deps)
		return target
	}
	voids := target("voids", api.TargetVoid, "c:lib")
	voids.UseRequires()
	targets := map[string]map[string]*api.Target{
		"a": {
			"lib":   target("lib", api.TargetStatic, "voids"),
			"app":   target("app", api.TargetBinary, "b:lib", "c:*"),
			"voids": voids,
		},
		"b": {"lib": target("lib", api.TargetStatic)},
		"c": {"lib": target("lib", api.TargetStatic)},
	}
	meta := map[string]PkgBuildMeta{
		"a": {Deps: []string{"b"}},
		"b": {},
		"c": {},
	}
	strict, err := NewBuildGraph(targets, meta, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	audit := AuditBuildGraph(targets, meta)
	if len(strict.Nodes) != len(audit.Nodes) {
		t.Fatalf("node counts differ: strict=%d audit=%d", len(strict.Nodes), len(audit.Nodes))
	}
	for fullName, strictNode := range strict.Nodes {
		auditNode := audit.Nodes[fullName]
		if auditNode == nil {
			t.Fatalf("audit graph is missing %s", fullName)
		}
		if !slices.Equal(strictNode.Deps, auditNode.Deps) {
			t.Errorf("%s deps differ: strict=%v audit=%v", fullName, strictNode.Deps, auditNode.Deps)
		}
		if !slices.Equal(strictNode.DirectDeps, auditNode.DirectDeps) {
			t.Errorf("%s direct deps differ: strict=%v audit=%v", fullName, strictNode.DirectDeps, auditNode.DirectDeps)
		}
		if !slices.Equal(strictNode.DeclaredDepPkgs, auditNode.DeclaredDepPkgs) {
			t.Errorf("%s declared packages differ: strict=%v audit=%v", fullName, strictNode.DeclaredDepPkgs, auditNode.DeclaredDepPkgs)
		}
	}
	if !slices.Equal(strict.Order, audit.Order) {
		t.Errorf("order differs: strict=%v audit=%v", strict.Order, audit.Order)
	}
}
