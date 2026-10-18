package pipeline

import (
	"slices"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func targetTestMap(t *testing.T, targets ...*api.Target) map[string]*api.Target {
	t.Helper()
	result := make(map[string]*api.Target, len(targets))
	for _, target := range targets {
		result[target.Name()] = target
	}
	return result
}

func TestTargetListLinesSorted(t *testing.T) {
	ctx := api.NewBuildContext("fixture", nil)
	app := api.SnapshotTarget(ctx.Target("app").SetKind(api.TargetBinary))
	docs := api.SnapshotTarget(ctx.Target("docs").SetKind(api.TargetVoid).SetDefault(false))
	check := api.SnapshotTarget(ctx.Target("check").SetKind(api.TargetVoid).SetTest(true))
	zlib := api.SnapshotTarget(ctx.Target("zlib").SetKind(api.TargetStatic))
	alpha := api.SnapshotTarget(ctx.Target("alpha").SetKind(api.TargetVoid))

	allTargets := map[string]map[string]*api.Target{
		"zeta":  targetTestMap(t, zlib, alpha),
		"alpha": targetTestMap(t, docs, app),
		"beta":  targetTestMap(t, check),
	}
	want := []string{
		"  - alpha:app (binary)",
		"  - alpha:docs (void) [disabled]",
		"  - beta:check (void) [test]",
		"  - zeta:alpha (void)",
		"  - zeta:zlib (static)",
	}
	for i := 0; i < 50; i++ {
		if got := targetListLines(allTargets); !slices.Equal(got, want) {
			t.Fatalf("targetListLines() = %q, want %q", got, want)
		}
	}
}

func TestTargetListLinesEmpty(t *testing.T) {
	if got := targetListLines(map[string]map[string]*api.Target{}); len(got) != 0 {
		t.Fatalf("targetListLines() = %q, want empty", got)
	}
}
