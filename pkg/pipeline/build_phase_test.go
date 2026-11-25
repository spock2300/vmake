package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spock2300/vmake/pkg/config"
)

func newStageTestContext(t *testing.T) *RuntimeContext {
	t.Helper()
	root := t.TempDir()
	buildGo := `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("option").SetType(api.OptionString).SetDefault("default")
	})
}
`
	if err := os.WriteFile(filepath.Join(root, "build.go"), []byte(buildGo), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := &RuntimeContext{
		Context: context.Background(),
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
	if err := Configure(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func neededPackages(ctx *RuntimeContext) map[string]bool {
	needed := make(map[string]bool, len(ctx.DepGraph.Packages))
	for name := range ctx.DepGraph.Packages {
		needed[name] = true
	}
	return needed
}

func TestPrepareConfigUsesPreludeFlagsAndNeededSet(t *testing.T) {
	ctx := newStageTestContext(t)
	needed := neededPackages(ctx)
	pre := &buildPrelude{
		needed:          needed,
		effectiveFlags:  [4][]string{{"-DPre"}, {"-DPreCxx"}},
		globalFlagsHash: "prelude-hash",
	}

	rc := newRunContext(ctx, BuildOptions{}, nil)
	conf, err := prepareConfig(rc, needed, pre)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rc.globalFlags, pre.effectiveFlags) {
		t.Fatalf("globalFlags = %v, want the prelude's effective flags %v", rc.globalFlags, pre.effectiveFlags)
	}
	if rc.globalFlagsHash != pre.globalFlagsHash {
		t.Fatalf("globalFlagsHash = %q, want %q", rc.globalFlagsHash, pre.globalFlagsHash)
	}
	if len(conf.allPkgOptions) != len(needed) {
		t.Fatalf("allPkgOptions covers %d packages, want %d", len(conf.allPkgOptions), len(needed))
	}
	for name := range needed {
		if _, ok := conf.allPkgOptions[name]; !ok {
			t.Fatalf("allPkgOptions missing needed package %q", name)
		}
	}
}

func TestPrepareConfigAggregatesWithoutPrelude(t *testing.T) {
	ctx := newStageTestContext(t)
	needed := neededPackages(ctx)

	rc := newRunContext(ctx, BuildOptions{}, nil)
	conf, err := prepareConfig(rc, needed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rc.globalFlagsHash == "" {
		t.Fatal("globalFlagsHash is empty without a prelude")
	}
	if len(conf.allPkgOptions) != len(needed) {
		t.Fatalf("allPkgOptions covers %d packages, want %d", len(conf.allPkgOptions), len(needed))
	}
	for name := range needed {
		if _, ok := conf.allPkgOptions[name]; !ok {
			t.Fatalf("allPkgOptions missing needed package %q", name)
		}
	}
}
