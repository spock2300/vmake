package main

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestApplyEphemeralOverrideRequiresCanonicalPackageID(t *testing.T) {
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
	if err := applyEphemeralOverrides(ctx, []inspectOverride{{pkgName: "kernel", optName: "debug", raw: "true"}}); err == nil {
		t.Fatal("short package name override unexpectedly succeeded")
	}
	if err := applyEphemeralOverrides(ctx, []inspectOverride{{pkgName: name, optName: "debug", raw: "true"}}); err != nil {
		t.Fatal(err)
	}
	if got := config.GetEntry(ctx.Config, name).Options["debug"]; got != true {
		t.Fatalf("%s:debug = %v, want true", name, got)
	}
}
