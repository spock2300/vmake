package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

func TestApplyConfigurationOwnsSnapshots(t *testing.T) {
	ctx := &RuntimeContext{}
	cfg := Configuration{
		AllOptions:          map[string]map[string]*api.Option{"app": {"opt": (&api.Option{}).SetType(api.OptionString)}},
		DerivedOptions:      map[string]map[string]any{"app": {"derived": 1}},
		GlobalMacroDefines:  []string{"A=1"},
		BufferedGlobalFlags: map[string]*packageGlobalFlags{"app": {cFlags: []string{"-DX"}}},
	}
	ctx.applyConfiguration(cfg)
	got := ctx.Configuration()
	if got.AllOptions["app"]["opt"] == nil || got.GlobalMacroDefines[0] != "A=1" {
		t.Fatalf("configuration = %+v", got)
	}
	// Mutating the accessor copy must not mutate the facade or its owned maps.
	got.AllOptions["app"]["opt"] = nil
	got.GlobalMacroDefines[0] = "B=2"
	got.BufferedGlobalFlags["app"].cFlags[0] = "-DY"
	if ctx.AllOptions["app"]["opt"] == nil {
		t.Fatal("accessor mutation changed AllOptions")
	}
	if ctx.GlobalMacroDefines[0] != "A=1" {
		t.Fatal("accessor mutation changed GlobalMacroDefines")
	}
	if ctx.BufferedGlobalFlags["app"].cFlags[0] != "-DX" {
		t.Fatal("accessor mutation changed BufferedGlobalFlags")
	}
}

func TestApplyPreparationOwnsSnapshots(t *testing.T) {
	ctx := &RuntimeContext{}
	prep := Preparation{
		Needed:         map[string]bool{"app": true},
		RawFlags:       [4][]string{{"-DRaw"}},
		EffectiveFlags: [4][]string{{"-DEff"}},
	}
	ctx.applyPreparation(prep)
	got := ctx.Preparation()
	got.Needed["app"] = false
	got.EffectiveFlags[0][0] = "-DMutated"
	if !ctx.Preparation().Needed["app"] {
		t.Fatal("accessor mutation changed Needed")
	}
	if ctx.Preparation().EffectiveFlags[0][0] != "-DEff" {
		t.Fatal("accessor mutation changed EffectiveFlags")
	}
}

func TestRequireAppliesResolution(t *testing.T) {
	root := t.TempDir()
	buildGo := `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {}
`
	if err := os.WriteFile(filepath.Join(root, "build.go"), []byte(buildGo), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := &RuntimeContext{
		WorkDir: root,
		Paths: &Paths{
			ProjectDir: root,
			DepsDir:    filepath.Join(root, ".vmake_deps"),
			CacheDir:   filepath.Join(root, "cache"),
			ReposDir:   filepath.Join(root, "repos"),
		},
	}
	if err := Require(ctx); err != nil {
		t.Fatal(err)
	}
	resolution := ctx.Resolution()
	if resolution.Resolver == nil || resolution.DepGraph == nil {
		t.Fatalf("resolution = %+v", resolution)
	}
	if ctx.Resolver != resolution.Resolver || ctx.DepGraph != resolution.DepGraph {
		t.Fatal("facade fields do not match the Resolution result")
	}
}

func TestConfigureRollbackOnFailure(t *testing.T) {
	root := t.TempDir()
	buildGo := `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("option").SetType(api.OptionString).SetDefault("default")
	})
}
`
	if err := os.WriteFile(filepath.Join(root, "build.go"), []byte(buildGo), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := &RuntimeContext{
		WorkDir: root,
		Config: &config.ConfigFile{
			Global:  &config.GlobalConfig{Options: map[string]any{}},
			Entries: map[string]*config.EntryConfig{},
		},
		Paths: &Paths{
			ProjectDir: root,
			DepsDir:    filepath.Join(root, ".vmake_deps"),
			CacheDir:   filepath.Join(root, "cache"),
			ReposDir:   filepath.Join(root, "repos"),
		},
	}
	if err := Require(ctx); err != nil {
		t.Fatal(err)
	}
	beforeConfiguration := ctx.Configuration()
	beforeConfig := ctx.Config.Clone()
	ctx.OnOptionsCollected = func(c *RuntimeContext) error {
		c.Config.Global.Options["injected"] = "value"
		return errors.New("reject configuration")
	}
	if err := Configure(ctx); err == nil {
		t.Fatal("Configure succeeded despite the rejected callback")
	}
	if got := ctx.Configuration(); !reflect.DeepEqual(got, beforeConfiguration) {
		t.Fatalf("failed Configure committed configuration: %+v", got)
	}
	if !reflect.DeepEqual(ctx.Config, beforeConfig) {
		t.Fatalf("failed Configure mutated Config: %+v", ctx.Config)
	}
}
