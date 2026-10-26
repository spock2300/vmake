package pipeline

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

func TestFilterDependenciesSeesPackageGlobalOption(t *testing.T) {
	for _, test := range []struct {
		name   string
		global string
		entry  string
		needed bool
	}{
		{name: "configured global", global: "chip-b", needed: true},
		{name: "default global", needed: false},
		{name: "configured entry", entry: "chip-b", needed: true},
		{name: "entry overrides global", global: "chip-a", entry: "chip-b", needed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := sessionFixture(t)
			app := s.ctx.DepGraph.Packages["app"]
			app.Pkg.SetRoot(true)
			app.Pkg.OnConfig(func(ctx *api.ConfigContext) {
				ctx.GlobalOption(api.ChipOptionName).SetType(api.OptionChoice).
					SetDefault("chip-a").SetValues("chip-a", "chip-b")
			})
			app.Pkg.OnRequire(func(ctx *api.RequireContext) {
				if ctx.When(api.ChipOptionName, "chip-b") {
					ctx.AddRequires("dep")
				}
			})
			if err := runConfigPhase(s.ctx); err != nil {
				t.Fatal(err)
			}
			if test.global != "" {
				s.ctx.Config.Global.Options[api.ChipOptionName] = test.global
			}
			if test.entry != "" {
				config.SetEntry(s.ctx.Config, "app", &config.EntryConfig{
					Options: map[string]any{api.ChipOptionName: test.entry},
				})
			}
			if err := filterDependencies(s.ctx, false); err != nil {
				t.Fatal(err)
			}
			if got := containsString(app.Deps, "dep"); got != test.needed {
				t.Fatalf("dep needed = %v, want %v (deps=%v)", got, test.needed, app.Deps)
			}
		})
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
