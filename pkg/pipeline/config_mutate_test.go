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

func TestResolveSetPackageName(t *testing.T) {
	ctx := &RuntimeContext{allOptions: map[string]map[string]*api.Option{
		"app":               {"debug": testBoolOption()},
		"vendor/app":        {"debug": testBoolOption()},
		"vendor/sdk/driver": {"debug": testBoolOption()},
		"vendor/a/kernel":   {"debug": testBoolOption()},
		"vendor/b/kernel":   {"debug": testBoolOption()},
	}}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "exact", in: "app", want: "app"},
		{name: "full", in: "vendor/sdk/driver", want: "vendor/sdk/driver"},
		{name: "unique suffix", in: "driver", want: "vendor/sdk/driver"},
		{name: "ambiguous suffix", in: "kernel", want: "kernel"},
		{name: "partial path", in: "sdk/driver", want: "sdk/driver"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ResolveSetPackageName(ctx, test.in); got != test.want {
				t.Fatalf("ResolveSetPackageName(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestResolveSetPackageOptionShortNameAndMutateConfig(t *testing.T) {
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
	resolved, opt, err := ResolveSetPackageOption(ctx, "kernel", "debug")
	if err != nil || resolved != name || opt == nil || opt.Type() != api.OptionBool {
		t.Fatalf("resolved = %q, opt = %+v, err = %v", resolved, opt, err)
	}

	changes, err := MutateConfig(ctx, ConfigMutation{
		Unsets: []UnsetItem{{Pkg: "stale/pkg", Opt: "old"}},
		Sets:   []SetItem{{Pkg: "kernel", Opt: "debug", Raw: "true"}},
	})
	if err != nil {
		t.Fatalf("MutateConfig: %v", err)
	}
	if len(changes) != 2 || changes[1].Pkg != name || changes[1].Value != true {
		t.Fatalf("changes = %+v", changes)
	}
	if ctx.Config.HasExplicitOption("stale/pkg", "old") {
		t.Fatal("stale explicit key should be unset")
	}
	if got := config.GetEntry(ctx.Config, name).Options["debug"]; got != true {
		t.Fatalf("%s/debug = %v, want true", name, got)
	}

	ctx.allOptions["vendor/other/kernel"] = map[string]*api.Option{"debug": testBoolOption()}
	if _, _, err := ResolveSetPackageOption(ctx, "kernel", "debug"); err == nil || !strings.Contains(err.Error(), `package "kernel" not found`) {
		t.Fatalf("ambiguous short name error = %v", err)
	}
}
