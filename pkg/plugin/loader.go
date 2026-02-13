package plugin

import (
	"fmt"
	"plugin"

	"gitee.com/spock2300/vmake/pkg/api"
)

type LoadedPlugin struct {
	Package   Package
	Builder   *api.Builder
	PluginPtr *plugin.Plugin
}

type LoadResult struct {
	Package Package
	Loaded  *LoadedPlugin
	Success bool
	Error   error
}

func Load(pluginPath string, pkg Package) LoadResult {
	p, err := plugin.Open(pluginPath)
	if err != nil {
		return LoadResult{
			Package: pkg,
			Success: false,
			Error:   fmt.Errorf("failed to open plugin: %w", err),
		}
	}

	mainSym, err := p.Lookup("Main")
	if err != nil {
		return LoadResult{
			Package: pkg,
			Success: false,
			Error:   fmt.Errorf("failed to find Main function: %w", err),
		}
	}

	mainFunc, ok := mainSym.(func(*api.Builder))
	if !ok {
		return LoadResult{
			Package: pkg,
			Success: false,
			Error:   fmt.Errorf("Main has wrong type signature"),
		}
	}

	builder := &api.Builder{}
	mainFunc(builder)

	return LoadResult{
		Package: pkg,
		Loaded: &LoadedPlugin{
			Package:   pkg,
			Builder:   builder,
			PluginPtr: p,
		},
		Success: true,
	}
}

func LoadAll(compileResults []CompileResult) []LoadResult {
	results := make([]LoadResult, len(compileResults))
	for i, cr := range compileResults {
		if !cr.Success {
			results[i] = LoadResult{
				Package: cr.Package,
				Success: false,
				Error:   fmt.Errorf("compilation failed, skipping load"),
			}
			continue
		}
		results[i] = Load(cr.PluginPath, cr.Package)
	}
	return results
}
