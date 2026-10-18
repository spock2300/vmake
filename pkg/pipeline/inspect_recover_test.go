package pipeline

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

func TestInspectFilterDepsRecoversOnRequirePanic(t *testing.T) {
	s := sessionFixture(t)
	config.SetEntry(s.ctx.Config, "app", &config.EntryConfig{Options: map[string]any{}})
	s.ctx.DepGraph.Packages["app"].Pkg.OnRequire(func(ctx *api.RequireContext) {
		ctx.When("nonexistent_option", true)
	})

	_, err := InspectWithOptions(s.ctx, InspectOptions{FilterDeps: true})
	if err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("InspectWithOptions error = %v, want unknown option error", err)
	}
}
