package main

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestApplyEphemeralOverrideShortPackageName(t *testing.T) {
	name := "vendor/sdk/kernel"
	pkg := api.NewPackage()
	pkg.SetName(name)
	pkg.Option("debug").SetType(api.OptionBool).SetDefault(false)
	ctx := &RuntimeContext{
		Config: &config.ConfigFile{
			Global:  &config.GlobalConfig{Options: map[string]any{}},
			Entries: map[string]*config.EntryConfig{},
		},
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			name: {ID: name, Pkg: pkg},
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
