package pipeline

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func testBoolOption() *api.Option {
	return (&api.Option{}).SetType(api.OptionBool)
}

func TestResolveSetPackageOptionCanonicalIDAndMutateConfig(t *testing.T) {
	name := "vendor/sdk/kernel"
	ctx := &RuntimeContext{
		Config: &config.ConfigFile{
			Global:  &config.GlobalConfig{Options: map[string]any{}},
			Entries: map[string]*config.EntryConfig{"stale/pkg": {Options: map[string]any{"old": true}}},
		},
		allOptions: map[string]map[string]*api.Option{
			name: {"debug": testBoolOption()},
		},
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			name: {ID: name},
		}},
	}
	resolved, opt, err := ResolveSetPackageOption(ctx, name, "debug")
	if err != nil || resolved != name || opt == nil || opt.Type() != api.OptionBool {
		t.Fatalf("resolved = %q, opt = %+v, err = %v", resolved, opt, err)
	}

	changes, err := MutateConfig(ctx, ConfigMutation{
		Unsets: []UnsetItem{{Pkg: "stale/pkg", Opt: "old"}},
		Sets:   []SetItem{{Pkg: name, Opt: "debug", Raw: "true"}},
	})
	if err != nil {
		t.Fatalf("MutateConfig: %v", err)
	}
	if len(changes) != 2 || changes[1].Pkg != name || changes[1].Value != true || changes[1].Key() != name+":debug" {
		t.Fatalf("changes = %+v", changes)
	}
	if ctx.Config.HasExplicitOption("stale/pkg", "old") {
		t.Fatal("stale explicit key should be unset")
	}
	if got := config.GetEntry(ctx.Config, name).Options["debug"]; got != true {
		t.Fatalf("%s:debug = %v, want true", name, got)
	}

	if _, _, err := ResolveSetPackageOption(ctx, "kernel", "debug"); err == nil || !strings.Contains(err.Error(), `package "kernel" not found`) {
		t.Fatalf("unqualified short name should be rejected, got %v", err)
	}
}
