package pipeline

import (
	"fmt"
	"runtime"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func projectGlobalValues(ctx *RuntimeContext) map[string]any {
	values := make(map[string]any)
	for name, opt := range ctx.GlobalOptions {
		values[name] = opt.Default()
	}
	for name, value := range config.BuildGlobalValues(ctx.Config) {
		if value == nil {
			continue
		}
		if value == "" && isTargetOption(name) {
			continue
		}
		values[name] = value
	}
	return values
}

func isTargetOption(name string) bool {
	return name == api.TargetOSOptionName || name == api.TargetTripleOptionName
}

func platformFromValues(values map[string]any) api.Platform {
	os, _ := values[api.TargetOSOptionName].(string)
	triple, _ := values[api.TargetTripleOptionName].(string)
	if os == "" || os == "host" {
		os = runtime.GOOS
	}
	return api.Platform{OS: os, Triple: triple}
}

func toolchainTargetDefaults(name string) (string, string) {
	if name == "" || name == "host" {
		return "", ""
	}
	tc, err := toolchain.GetManager().GetToolchain(name)
	if err != nil || tc == nil {
		return "", ""
	}
	return tc.TargetOS, tc.TargetTriple
}

func ToolchainTargetDefaults(name string) map[string]any {
	os, triple := toolchainTargetDefaults(name)
	defaults := make(map[string]any, 2)
	if os != "" {
		defaults[api.TargetOSOptionName] = os
	}
	if triple != "" {
		defaults[api.TargetTripleOptionName] = triple
	}
	return defaults
}

func fillTargetDefaults(values map[string]any, os, triple string) {
	for name, def := range map[string]string{
		api.TargetOSOptionName:     os,
		api.TargetTripleOptionName: triple,
	} {
		if def == "" {
			continue
		}
		if value, ok := values[name]; ok && value != nil && value != "" {
			continue
		}
		values[name] = def
	}
}

func ProjectPlatform(ctx *RuntimeContext) (api.Platform, error) {
	values := projectGlobalValues(ctx)
	targetOS, triple := toolchainTargetDefaults(ResolveToolchainName(ctx.Config, ctx.ToolchainOverride))
	fillTargetDefaults(values, targetOS, triple)
	return checkedPlatform(values, "project")
}

func checkedPlatform(values map[string]any, owner string) (api.Platform, error) {
	for _, name := range []string{api.TargetOSOptionName, api.TargetTripleOptionName} {
		if value := values[name]; value != nil {
			if _, ok := value.(string); !ok {
				return api.Platform{}, fmt.Errorf("%s option %s must be a string", owner, name)
			}
		}
	}
	return platformFromValues(values), nil
}

func PackagePlatform(ctx *RuntimeContext, name string) (api.Platform, error) {
	values := projectGlobalValues(ctx)
	values[api.ToolchainOptionName] = ResolveToolchainName(ctx.Config, ctx.ToolchainOverride)
	if _, err := checkedPlatform(values, "project"); err != nil {
		return api.Platform{}, err
	}
	return checkedPlatform(packagePlatformValues(ctx, name, values), "package "+name)
}

func packagePlatformValues(ctx *RuntimeContext, name string, base map[string]any) map[string]any {
	values := map[string]any{api.TargetOSOptionName: "", api.TargetTripleOptionName: ""}
	opts := ctx.AllOptions[name]
	if opts == nil && ctx.DepGraph != nil {
		if node := ctx.DepGraph.Packages[name]; node != nil && node.Pkg != nil {
			opts = node.Pkg.GetOptions()
		}
	}
	for _, key := range []string{api.TargetOSOptionName, api.TargetTripleOptionName} {
		if value := base[key]; value != nil && value != "" {
			values[key] = value
		}
		if opt := opts[key]; opt != nil && !opt.IsGlobal() {
			if def := opt.Default(); def != nil && def != "" {
				values[key] = def
			}
		}
		if value := config.GetEntry(ctx.Config, name).Options[key]; value != nil && value != "" {
			values[key] = value
		}
	}
	defaultTC := ResolveToolchainName(ctx.Config, ctx.ToolchainOverride)
	if tc, ok := base[api.ToolchainOptionName].(string); ok && tc != "" {
		defaultTC = tc
	}
	targetOS, triple := toolchainTargetDefaults(resolvePkgToolchain(ctx.Config, name, defaultTC))
	fillTargetDefaults(values, targetOS, triple)
	return values
}
