package pipeline

import (
	"strings"
	"testing"
)

func newTestGraphValidator(needed map[string]bool, visPath map[string]string) (*graphValidator, *[]GraphIssue) {
	issues := &[]GraphIssue{}
	lookup := func(pkg string) string { return visPath[pkg] }
	return newGraphValidator(needed, lookup, func(issue GraphIssue) {
		*issues = append(*issues, issue)
	}), issues
}

func TestGraphValidatorCrossPkgOnBuildReportsMissingDependency(t *testing.T) {
	validator, issues := newTestGraphValidator(map[string]bool{"app": true}, nil)

	if validator.crossPkgOnBuild("app", "dep", "app:app", "dep") {
		t.Fatal("crossPkgOnBuild allowed a reference to an unneeded package")
	}
	if len(*issues) != 1 {
		t.Fatalf("issues = %+v, want exactly one", *issues)
	}
	issue := (*issues)[0]
	if issue.Kind != "missing-dependency" || issue.Severity != "error" || issue.Package != "app" || issue.Target != "app:app" {
		t.Fatalf("issue = %+v", issue)
	}
	if !strings.Contains(issue.Message, `AddRequires("dep")`) {
		t.Fatalf("message = %q, want the AddRequires remediation", issue.Message)
	}
}

func TestGraphValidatorCrossPkgOnBuildAllowsDeclaredSelfAndEmpty(t *testing.T) {
	validator, issues := newTestGraphValidator(map[string]bool{"app": true, "dep": true}, nil)

	for _, tc := range []struct {
		name                string
		from, to, ref, ref2 string
	}{
		{"declared", "app", "dep", "app:app", "dep"},
		{"self", "app", "app", "app:app", "app"},
		{"empty target", "", "dep", "app:app", "dep"},
		{"empty source", "app", "", "app:app", "dep"},
	} {
		if !validator.crossPkgOnBuild(tc.from, tc.to, tc.ref, tc.ref2) {
			t.Fatalf("%s: reference rejected", tc.name)
		}
	}
	if len(*issues) != 0 {
		t.Fatalf("issues = %+v, want none", *issues)
	}

	unneeded := map[string]bool{"dep": true}
	validator, issues = newTestGraphValidator(unneeded, nil)
	if !validator.crossPkgOnBuild("app", "dep", "app:app", "dep") {
		t.Fatal("reference from an unneeded package was rejected")
	}
	if len(*issues) != 0 {
		t.Fatalf("issues = %+v, want none", *issues)
	}
}

func TestGraphValidatorInternalVisibilityDedupesPerPair(t *testing.T) {
	validator, issues := newTestGraphValidator(
		map[string]bool{"app": true},
		map[string]string{"app": "apps/app", "dep": "hl/sdk/internal/core"},
	)

	validator.crossPkgOnBuild("app", "dep", "app:app", "dep")
	validator.crossPkgOnBuild("app", "dep", "app:other", "dep")

	var internal, missing int
	for _, issue := range *issues {
		switch issue.Kind {
		case "internal-visibility":
			internal++
			want := `use of internal package "dep" not allowed from package "app"`
			if issue.Message != want {
				t.Fatalf("message = %q, want %q", issue.Message, want)
			}
			if issue.Target != "app:app" {
				t.Fatalf("target = %q, want the first target that reached the pair", issue.Target)
			}
		case "missing-dependency":
			missing++
		}
	}
	if internal != 1 {
		t.Fatalf("internal-visibility issues = %d, want 1 (deduped per package pair)", internal)
	}
	if missing != 2 {
		t.Fatalf("missing-dependency issues = %d, want 2 (one per target)", missing)
	}
}

func TestGraphValidatorInternalVisibilityIgnoresExportedPackages(t *testing.T) {
	validator, issues := newTestGraphValidator(
		map[string]bool{"app": true},
		map[string]string{"app": "apps/app", "dep": "hl/sdk/core"},
	)

	validator.internalVisibility("app", "dep", "app:app")
	if len(*issues) != 0 {
		t.Fatalf("issues = %+v, want none for an exported package", *issues)
	}
}

func TestCheckGraphCyclesReportsPackageCycle(t *testing.T) {
	g := &UnifiedGraph{
		PackageDeps: map[string][]string{"a": {"b"}, "b": {"a"}},
		TargetDeps:  map[string][]string{},
	}
	issues := checkGraphCycles(g, func(from, to string) []string {
		return []string{from + " -> " + to}
	})
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want one package cycle", issues)
	}
	if issues[0].Kind != "package-cycle" || issues[0].Severity != "error" {
		t.Fatalf("issue = %+v", issues[0])
	}
	if !strings.Contains(issues[0].Message, "package circular dependency detected:") {
		t.Fatalf("message = %q", issues[0].Message)
	}
	if !strings.Contains(issues[0].Message, "(") {
		t.Fatalf("message = %q, want the edge details", issues[0].Message)
	}
}

func TestCheckGraphCyclesReportsTargetCycle(t *testing.T) {
	g := &UnifiedGraph{
		PackageDeps: map[string][]string{"p": nil},
		TargetDeps:  map[string][]string{"p:a": {"p:b"}, "p:b": {"p:a"}},
	}
	issues := checkGraphCycles(g, nil)
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want one target cycle", issues)
	}
	issue := issues[0]
	if issue.Kind != "target-cycle" || issue.Package != "p" || !strings.HasPrefix(issue.Target, "p:") {
		t.Fatalf("issue = %+v", issue)
	}
	if !strings.Contains(issue.Message, "target circular dependency detected:") {
		t.Fatalf("message = %q", issue.Message)
	}
}

func TestCheckGraphCyclesAcceptsAcyclicGraph(t *testing.T) {
	g := &UnifiedGraph{
		PackageDeps: map[string][]string{"app": {"lib"}, "lib": nil},
		TargetDeps:  map[string][]string{"app:app": {"lib:lib"}, "lib:lib": nil},
	}
	if issues := checkGraphCycles(g, nil); len(issues) != 0 {
		t.Fatalf("issues = %+v, want none", issues)
	}
}
