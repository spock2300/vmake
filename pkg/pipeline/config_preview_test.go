package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

func TestNewConfigPreviewerEvaluatesDraftWithoutMutatingEagerGraph(t *testing.T) {
	s := sessionFixture(t)
	app := s.ctx.DepGraph.Packages["//app"]
	dep := s.ctx.DepGraph.Packages["//dep"]
	usbPkg := localNode("//usb-stack")
	usbPkg.Source.Dir = t.TempDir()
	s.ctx.DepGraph.Packages["//usb-stack"] = usbPkg
	s.ctx.DepGraph.Roots = []string{"//app"}

	app.Pkg.AddRequires("//dep")
	app.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("usb").SetDefault(false)
		ctx.Option("diag").SetDefault(true)
	})
	app.Pkg.OnRequire(func(ctx *api.RequireContext) {
		if ctx.When("usb", true) {
			ctx.AddRequires("//usb-stack")
		}
	})
	app.Pkg.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		ctx.SetDefault("//dep", "shell", ctx.Bool("diag"))
		if ctx.Bool("usb") {
			ctx.SetDefault("//usb-stack", "cdc", true)
		}
	})
	dep.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("shell").SetDefault(false)
	})
	usbPkg.Pkg.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("cdc").SetDefault(false)
	})

	app.Deps = []string{"//dep", "//usb-stack"}
	if err := s.ctx.Resolver.UpdateOrder(); err != nil {
		t.Fatal(err)
	}
	if err := runConfigPhase(s.ctx); err != nil {
		t.Fatal(err)
	}

	s.ctx.ConfigPath = filepath.Join(s.ctx.WorkDir, "configs", "wifi-ble.json")
	if wb := NewConfigWorkbench(s.ctx); wb == nil || wb.ConfigFile != "configs/wifi-ble.json" {
		t.Fatalf("NewConfigWorkbench ConfigFile = %v, want configs/wifi-ble.json", wb)
	}

	preview := NewConfigPreviewer(s.ctx)
	s.ctx.Resolver.Close()
	s.ctx.Resolver = nil

	p0, ok := preview(s.ctx.Config)
	if !ok {
		t.Fatal("initial preview failed")
	}
	if !p0.ActivePackages["//app"] || !p0.ActivePackages["//dep"] || p0.ActivePackages["//usb-stack"] {
		t.Fatalf("unexpected initial active packages: %v", p0.ActivePackages)
	}
	if p0.DerivedOptions["//dep"]["shell"] != true || p0.DerivedOwners["//dep"]["shell"] != "//app" {
		t.Fatalf("expected //dep:shell=true derived by //app, got val=%v owner=%q", p0.DerivedOptions["//dep"]["shell"], p0.DerivedOwners["//dep"]["shell"])
	}

	draft := s.ctx.Config.Clone()
	config.SetEntry(draft, "//app", &config.EntryConfig{
		Options: map[string]any{"usb": true},
	})
	p1, ok := preview(draft)
	if !ok {
		t.Fatal("draft preview failed")
	}
	if !p1.ActivePackages["//usb-stack"] {
		t.Fatalf("expected //usb-stack active when usb=true: %v", p1.ActivePackages)
	}
	if p1.DerivedOptions["//usb-stack"]["cdc"] != true || p1.DerivedOwners["//usb-stack"]["cdc"] != "//app" {
		t.Fatalf("expected //usb-stack:cdc=true derived by //app, got val=%v owner=%q", p1.DerivedOptions["//usb-stack"]["cdc"], p1.DerivedOwners["//usb-stack"]["cdc"])
	}

	if s.ctx.DepGraph.IsFrozen() {
		t.Fatal("preview must not freeze DepGraph")
	}
	if !containsString(app.Deps, "//usb-stack") {
		t.Fatalf("eager app.Deps was mutated: %v", app.Deps)
	}
}
