package pipeline

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func registerTargetDefaultsToolchain(t *testing.T, prefix, targetOS, triple string) string {
	t.Helper()
	name := prefix + "-" + storage.OwnerKey(filepath.ToSlash(t.TempDir()))[:16]
	tc := &toolchain.Toolchain{Name: name, TargetOS: targetOS, TargetTriple: triple}
	if err := toolchain.GetManager().RegisterToolchain(name, tc); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestProjectPlatformUsesToolchainTargetDefaults(t *testing.T) {
	tcName := registerTargetDefaultsToolchain(t, "target-defaults-project", "none", "arm-none-eabi")

	for _, test := range []struct {
		name     string
		declared map[string]string
		global   map[string]any
		want     api.Platform
	}{
		{name: "toolchain defaults", want: api.Platform{OS: "none", Triple: "arm-none-eabi"}},
		{name: "declared defaults win", declared: map[string]string{api.TargetOSOptionName: "linux", api.TargetTripleOptionName: "aarch64-linux-gnu"}, want: api.Platform{OS: "linux", Triple: "aarch64-linux-gnu"}},
		{name: "global config wins", global: map[string]any{api.TargetOSOptionName: "windows", api.TargetTripleOptionName: "mingw"}, want: api.Platform{OS: "windows", Triple: "mingw"}},
		{name: "empty global config falls back", global: map[string]any{api.TargetOSOptionName: "", api.TargetTripleOptionName: ""}, want: api.Platform{OS: "none", Triple: "arm-none-eabi"}},
		{name: "empty global config keeps declared default", declared: map[string]string{api.TargetOSOptionName: "linux", api.TargetTripleOptionName: ""}, global: map[string]any{api.TargetOSOptionName: "", api.TargetTripleOptionName: ""}, want: api.Platform{OS: "linux", Triple: "arm-none-eabi"}},
		{name: "host os keeps toolchain triple", declared: map[string]string{api.TargetOSOptionName: "host"}, want: api.Platform{OS: runtime.GOOS, Triple: "arm-none-eabi"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := api.NewConfigContext("app")
			for key, value := range test.declared {
				options.GlobalOption(key).SetType(api.OptionString).SetDefault(value)
			}
			node := localNode("app")
			ctx := &RuntimeContext{
				Config:        emptyConfig(),
				DepGraph:      &resolver.Graph{Packages: map[string]*resolver.PackageNode{"app": node}},
				AllOptions:    map[string]map[string]*api.Option{"app": options.GetOptions()},
				GlobalOptions: options.GetOptions(),
			}
			ctx.Config.Global.Toolchain = tcName
			ctx.Config.Global.Options = test.global

			platform, err := ProjectPlatform(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if platform != test.want {
				t.Errorf("project platform = %+v, want %+v", platform, test.want)
			}
		})
	}
}

func TestPackagePlatformUsesPackageToolchainTargetDefaults(t *testing.T) {
	projectTC := registerTargetDefaultsToolchain(t, "target-defaults-project-pkg", "none", "arm-none-eabi")
	packageTC := registerTargetDefaultsToolchain(t, "target-defaults-package", "windows", "x86_64-w64-mingw32")

	options := api.NewConfigContext("app")
	node := localNode("app")
	ctx := &RuntimeContext{
		Config:        emptyConfig(),
		DepGraph:      &resolver.Graph{Packages: map[string]*resolver.PackageNode{"app": node}},
		AllOptions:    map[string]map[string]*api.Option{"app": options.GetOptions()},
		GlobalOptions: options.GetOptions(),
	}
	ctx.Config.Global.Toolchain = projectTC
	config.SetEntry(ctx.Config, "app", &config.EntryConfig{Options: map[string]any{api.ToolchainOptionName: packageTC}})

	projectPlatform, err := ProjectPlatform(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if projectPlatform != (api.Platform{OS: "none", Triple: "arm-none-eabi"}) {
		t.Errorf("project platform = %+v", projectPlatform)
	}
	packagePlatform, err := PackagePlatform(ctx, "app")
	if err != nil {
		t.Fatal(err)
	}
	if packagePlatform != (api.Platform{OS: "windows", Triple: "x86_64-w64-mingw32"}) {
		t.Errorf("package platform = %+v", packagePlatform)
	}
	if _, ok := projectGlobalValues(ctx)[api.TargetOSOptionName]; ok {
		t.Error("project global values were mutated with toolchain defaults")
	}
}

func TestPackagePlatformUsesEffectiveToolchainSelection(t *testing.T) {
	first := registerTargetDefaultsToolchain(t, "target-defaults-first", "none", "arm-none-eabi")
	second := registerTargetDefaultsToolchain(t, "target-defaults-second", "windows", "x86_64-w64-mingw32")

	options := api.NewConfigContext("app")
	node := localNode("app")
	ctx := &RuntimeContext{
		Config:     emptyConfig(),
		DepGraph:   &resolver.Graph{Packages: map[string]*resolver.PackageNode{"app": node}},
		AllOptions: map[string]map[string]*api.Option{"app": options.GetOptions()},
	}
	var err error
	ctx.GlobalOptions, err = api.MergeGlobalOptions(ctx.AllOptions, []string{first, "host", second})
	if err != nil {
		t.Fatal(err)
	}

	projectPlatform, err := ProjectPlatform(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if projectPlatform != (api.Platform{OS: runtime.GOOS}) {
		t.Errorf("project platform = %+v, want host selection", projectPlatform)
	}
	packagePlatform, err := PackagePlatform(ctx, "app")
	if err != nil {
		t.Fatal(err)
	}
	if packagePlatform != projectPlatform {
		t.Errorf("package platform = %+v, want the effective selection %+v", packagePlatform, projectPlatform)
	}

	ctx.ToolchainOverride = second
	projectPlatform, err = ProjectPlatform(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if projectPlatform != (api.Platform{OS: "windows", Triple: "x86_64-w64-mingw32"}) {
		t.Errorf("project platform with override = %+v", projectPlatform)
	}
	packagePlatform, err = PackagePlatform(ctx, "app")
	if err != nil {
		t.Fatal(err)
	}
	if packagePlatform != projectPlatform {
		t.Errorf("package platform with override = %+v, want %+v", packagePlatform, projectPlatform)
	}
}

func TestPlatformIgnoresUnavailableToolchainDefaults(t *testing.T) {
	ctx := &RuntimeContext{
		Config:   emptyConfig(),
		DepGraph: &resolver.Graph{Packages: map[string]*resolver.PackageNode{"app": localNode("app")}},
	}
	ctx.Config.Global.Toolchain = "target-defaults-missing"
	platform, err := ProjectPlatform(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if platform != (api.Platform{OS: runtime.GOOS}) {
		t.Errorf("platform = %+v, want host fallback", platform)
	}
}
