package pipeline

import (
	"fmt"
	"runtime"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

func projectGlobalValues(ctx *RuntimeContext) map[string]any {
	values := make(map[string]any)
	for name, opt := range ctx.GlobalOptions {
		values[name] = opt.Default()
	}
	for name, value := range config.BuildGlobalValues(ctx.Config) {
		if value != nil {
			values[name] = value
		}
	}
	return values
}

func platformFromValues(values map[string]any) api.Platform {
	os, _ := values[api.TargetOSOptionName].(string)
	triple, _ := values[api.TargetTripleOptionName].(string)
	if os == "" || os == "host" {
		os = runtime.GOOS
	}
	return api.Platform{OS: os, Triple: triple}
}

func ProjectPlatform(ctx *RuntimeContext) (api.Platform, error) {
	values := projectGlobalValues(ctx)
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
		if value := base[key]; value != nil {
			values[key] = value
		}
		if opt := opts[key]; opt != nil && !opt.IsGlobal() && opt.Default() != nil {
			values[key] = opt.Default()
		}
		if value := config.GetEntry(ctx.Config, name).Options[key]; value != nil {
			values[key] = value
		}
	}
	return values
}
