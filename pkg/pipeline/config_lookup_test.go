package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestDeclareTargetsCrossPackageConfig(t *testing.T) {
	owner := localNode("owner")
	ownerAccessor := api.NewConfigAccessor(nil, nil)
	ownerOpts := map[string]*api.Option{
		"feature": ownerAccessor.Option("feature").SetDefault(true),
	}
	owner.Pkg.SetOptions(ownerOpts)

	reader := localNode("reader")
	reader.Pkg.OnBuild(func(ctx *api.BuildContext) {
		kind := api.TargetVoid
		if ctx.Bool("owner/feature") {
			kind = api.TargetObject
		}
		ctx.Target("main").SetKind(kind)
	})

	cfg := emptyConfig()
	ctx := &RuntimeContext{
		Config: cfg,
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"owner":  owner,
			"reader": reader,
		}},
		AllOptions: map[string]map[string]*api.Option{"owner": ownerOpts},
	}
	srcDir := t.TempDir()
	dirs := &api.PkgDirs{SourceDir: srcDir, BuildDir: filepath.Join(srcDir, "build", "key")}

	buildCtx, err := DeclareTargets(ctx, "reader", dirs, testToolchain(), map[string]any{"mode": "debug"})
	if err != nil {
		t.Fatal(err)
	}
	if kind := buildCtx.GetTargets()["main"].Kind(); kind != api.TargetObject {
		t.Fatalf("feature on target kind = %s, want object", kind)
	}

	cfg.Entries["owner"] = &config.EntryConfig{Options: map[string]any{"feature": false}}
	buildCtx, err = DeclareTargets(ctx, "reader", dirs, testToolchain(), map[string]any{"mode": "debug"})
	if err != nil {
		t.Fatal(err)
	}
	if kind := buildCtx.GetTargets()["main"].Kind(); kind != api.TargetVoid {
		t.Fatalf("feature off target kind = %s, want void", kind)
	}
}

func TestConfigLookupUsesBoundValuesAndReportsMissing(t *testing.T) {
	owner := localNode("owner")
	ownerAccessor := api.NewConfigAccessor(nil, nil)
	ownerOpts := map[string]*api.Option{
		"feature": ownerAccessor.Option("feature").SetDefault(true),
	}
	owner.Pkg.SetOptions(ownerOpts)

	ctx := &RuntimeContext{
		Config: emptyConfig(),
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"owner": owner,
		}},
		AllOptions: map[string]map[string]*api.Option{"owner": ownerOpts},
	}
	lookup := newConfigLookup(ctx, nil, func(pkg string) (map[string]any, bool) {
		if pkg != "owner" {
			return nil, false
		}
		return map[string]any{"feature": false}, true
	})
	value, opt, err := lookup("owner", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if value != false || opt == nil {
		t.Fatalf("bound lookup = %v/%v, want false with option", value, opt)
	}
	if _, _, err := lookup("missing", "feature"); err == nil {
		t.Error("unknown package should fail")
	}
	if _, _, err := lookup("owner", "missing"); err == nil {
		t.Error("unknown option should fail")
	}
}

func TestConfigLookupGlobalOption(t *testing.T) {
	reader := localNode("reader")
	globalAccessor := api.NewConfigAccessor(nil, nil)
	globalOpt := globalAccessor.Option("chip").SetDefault("py32f539").SetGroup(api.GroupGlobal)
	ctx := &RuntimeContext{
		Config: emptyConfig(),
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"reader": reader,
		}},
		AllOptions:    map[string]map[string]*api.Option{},
		GlobalOptions: map[string]*api.Option{"chip": globalOpt},
	}
	lookup := newConfigLookup(ctx, map[string]any{"chip": "py32f537"}, nil)
	value, opt, err := lookup("reader", "chip")
	if err != nil {
		t.Fatal(err)
	}
	if value != "py32f537" || opt != globalOpt {
		t.Fatalf("global lookup = %v/%v, want py32f537 with global option", value, opt)
	}
}

func TestConfigLookupGlobalOptionUsesPackageOverride(t *testing.T) {
	reader := localNode("reader")
	globalAccessor := api.NewConfigAccessor(nil, nil)
	globalOpt := globalAccessor.Option("chip").SetDefault("py32f539").SetGroup(api.GroupGlobal)
	cfg := emptyConfig()
	cfg.Entries["reader"] = &config.EntryConfig{Options: map[string]any{"chip": "py32f537"}}
	ctx := &RuntimeContext{
		Config: cfg,
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"reader": reader,
		}},
		AllOptions:    map[string]map[string]*api.Option{},
		GlobalOptions: map[string]*api.Option{"chip": globalOpt},
	}
	lookup := newConfigLookup(ctx, map[string]any{"chip": "py32f539"}, nil)
	value, opt, err := lookup("reader", "chip")
	if err != nil {
		t.Fatal(err)
	}
	if value != "py32f537" || opt != globalOpt {
		t.Fatalf("global lookup = %v/%v, want package override py32f537", value, opt)
	}
}

func TestConfigLookupImplicitPlatformOption(t *testing.T) {
	reader := localNode("reader")
	ctx := &RuntimeContext{
		Config: emptyConfig(),
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{
			"reader": reader,
		}},
		AllOptions:    map[string]map[string]*api.Option{},
		GlobalOptions: map[string]*api.Option{},
	}
	lookup := newConfigLookup(ctx, map[string]any{}, nil)
	value, opt, err := lookup("reader", api.TargetOSOptionName)
	if err != nil {
		t.Fatal(err)
	}
	if opt == nil || opt.Type() != api.OptionString {
		t.Fatalf("opt = %v, want a string option", opt)
	}
	if _, ok := value.(string); !ok {
		t.Fatalf("value = %#v, want a string", value)
	}
}
