package pipeline

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/issue"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

func makeAuditTarget(name string, deps ...string) *api.Target {
	tr := api.NewTargetRegistry()
	t := tr.Target(name)
	t.AddDeps(deps)
	return t
}

func TestAuditBuildGraphCrossPkgOnBuildReportsMissingDependency(t *testing.T) {
	bg := build.AuditBuildGraph(
		map[string]map[string]*api.Target{
			"app": {"app": makeAuditTarget("app", "dep")},
		},
		map[string]build.PkgBuildMeta{
			"app": {},
			"dep": {},
		},
		build.GraphAuditOptions{
			Needed:   map[string]bool{"app": true},
			Included: map[string]bool{"app": true},
		},
	)
	if len(bg.Issues) != 1 {
		t.Fatalf("issues = %+v, want exactly one", bg.Issues)
	}
	issue := bg.Issues[0]
	if issue.Kind != "missing-dependency" || issue.Severity != "error" || issue.Package != "app" || issue.Target != "app:app" {
		t.Fatalf("issue = %+v", issue)
	}
	if !strings.Contains(issue.Message, `AddRequires("dep")`) {
		t.Fatalf("message = %q, want the AddRequires remediation", issue.Message)
	}
}

func TestAuditBuildGraphCrossPkgOnBuildAllowsDeclaredAndSelf(t *testing.T) {
	bg := build.AuditBuildGraph(
		map[string]map[string]*api.Target{
			"app": {"app": makeAuditTarget("app", "dep:lib"), "self": makeAuditTarget("self", "app")},
			"dep": {"lib": makeAuditTarget("lib")},
		},
		map[string]build.PkgBuildMeta{
			"app": {Deps: []string{"dep"}},
			"dep": {},
		},
		build.GraphAuditOptions{
			Needed:   map[string]bool{"app": true, "dep": true},
			Included: map[string]bool{"app": true, "dep": true},
		},
	)
	if len(bg.Issues) != 0 {
		t.Fatalf("issues = %+v, want none", bg.Issues)
	}
}

func TestAuditBuildGraphInternalVisibilityDedupesPerPair(t *testing.T) {
	bg := build.AuditBuildGraph(
		map[string]map[string]*api.Target{
			"app": {
				"app":   makeAuditTarget("app", "dep"),
				"other": makeAuditTarget("other", "dep"),
			},
		},
		map[string]build.PkgBuildMeta{
			"app": {VisPath: "apps/app"},
			"dep": {VisPath: "hl/sdk/internal/core"},
		},
		build.GraphAuditOptions{
			Needed:   map[string]bool{"app": true},
			Included: map[string]bool{"app": true},
		},
	)

	var internal, missing int
	for _, issue := range bg.Issues {
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

func TestAuditBuildGraphReportsPackageAndTargetCycles(t *testing.T) {
	bg := build.AuditBuildGraph(
		map[string]map[string]*api.Target{
			"a": {"x": makeAuditTarget("x", "y"), "y": makeAuditTarget("y", "x")},
			"b": {},
		},
		map[string]build.PkgBuildMeta{
			"a": {Deps: []string{"b"}},
			"b": {Deps: []string{"a"}},
		},
	)
	var pkgCycles, targetCycles int
	for _, issue := range bg.Issues {
		switch issue.Kind {
		case "package-cycle":
			pkgCycles++
			if !strings.Contains(issue.Message, "package circular dependency detected:") || !strings.Contains(issue.Message, "(OnRequire") {
				t.Fatalf("message = %q", issue.Message)
			}
		case "target-cycle":
			targetCycles++
			if issue.Package != "a" || !strings.HasPrefix(issue.Target, "a:") || !strings.Contains(issue.Message, "target circular dependency detected:") {
				t.Fatalf("issue = %+v", issue)
			}
		}
	}
	if pkgCycles != 1 || targetCycles != 1 {
		t.Fatalf("issues = %+v, want 1 package-cycle and 1 target-cycle", bg.Issues)
	}
}

func TestIssueForReadsTypedKindsNotMessages(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"missing dependency", &build.ResolveError{Kind: build.IssueKindMissingDependency, Err: errors.New("whatever")}, "missing-dependency"},
		{"package cycle", &build.ResolveError{Kind: build.IssueKindPackageCycle, Err: errors.New("whatever")}, "package-cycle"},
		{"target cycle", &build.ResolveError{Kind: build.IssueKindTargetCycle, Err: errors.New("whatever")}, "target-cycle"},
		{"internal visibility", &build.ResolveError{Kind: build.IssueKindInternalVisibility, Err: errors.New("whatever")}, "internal-visibility"},
		{"require cycle", fmt.Errorf("%w: a -> b", issue.ErrCircularDependency), "package-cycle"},
		{"untyped", errors.New("circular dependency detected by nobody"), "resolve"},
	}
	for _, tc := range cases {
		if got := IssueFor(tc.err).Kind; got != tc.want {
			t.Errorf("%s: kind = %q, want %q", tc.name, got, tc.want)
		}
		if got := IssueFor(tc.err).Message; got != tc.err.Error() {
			t.Errorf("%s: message = %q, want %q", tc.name, got, tc.err.Error())
		}
	}
}
