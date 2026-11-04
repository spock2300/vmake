package main

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func boolOption() *api.Option {
	return (&api.Option{}).SetType(api.OptionBool)
}

func TestResolveSetPackageName(t *testing.T) {
	ctx := &RuntimeContext{AllOptions: map[string]map[string]*api.Option{
		"app":               {"debug": boolOption()},
		"vendor/app":        {"debug": boolOption()},
		"vendor/sdk/driver": {"debug": boolOption()},
		"vendor/a/kernel":   {"debug": boolOption()},
		"vendor/b/kernel":   {"debug": boolOption()},
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
			if got := resolveSetPackageName(ctx, test.in); got != test.want {
				t.Fatalf("resolveSetPackageName(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestResolveSetPackageOptionShortName(t *testing.T) {
	ctx := &RuntimeContext{AllOptions: map[string]map[string]*api.Option{
		"vendor/sdk/kernel": {"debug": boolOption()},
	}}
	resolved, opt, err := resolveSetPackageOption("kernel", "debug", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "vendor/sdk/kernel" || opt == nil || opt.Type() != api.OptionBool {
		t.Fatalf("resolved = %q, opt = %+v", resolved, opt)
	}

	ctx.AllOptions["vendor/other/kernel"] = map[string]*api.Option{"debug": boolOption()}
	if _, _, err := resolveSetPackageOption("kernel", "debug", ctx); err == nil || !strings.Contains(err.Error(), `package "kernel" not found`) {
		t.Fatalf("ambiguous short name error = %v", err)
	}
}

func TestApplyEphemeralOverrideShortPackageName(t *testing.T) {
	name := "vendor/sdk/kernel"
	ctx := &RuntimeContext{
		Config: &config.ConfigFile{
			Global:  &config.GlobalConfig{Options: map[string]any{}},
			Entries: map[string]*config.EntryConfig{},
		},
		AllOptions: map[string]map[string]*api.Option{
			name: {"debug": boolOption()},
		},
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			name: {ID: name},
		}},
	}
	overrides := []inspectOverride{{pkgName: "kernel", optName: "debug", raw: "true"}}
	if err := applyEphemeralOverrides(ctx, overrides); err != nil {
		t.Fatal(err)
	}
	if got := config.GetEntry(ctx.Config, name).Options["debug"]; got != true {
		t.Fatalf("%s/debug = %v, want true", name, got)
	}
}
