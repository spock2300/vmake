package plugin

import (
	"fmt"
	"path/filepath"

	"github.com/traefik/yaegi/interp"

	"github.com/spock2300/vmake/internal/gosrc"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/internal/yaegibase"
	"github.com/spock2300/vmake/internal/yaegisym"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type LoadedPlugin struct {
	Info  *Info
	Entry MainFunc
}

func pluginExports() interp.Exports {
	return interp.Exports{
		"github.com/spock2300/vmake/pkg/plugin/plugin":       YaegiSymbols(),
		"github.com/spock2300/vmake/pkg/toolchain/toolchain": toolchain.YaegiSymbols(),
		"github.com/spf13/cobra/cobra":                       yaegisym.Symbols["github.com/spf13/cobra/cobra"],
		"github.com/spf13/pflag/pflag":                       yaegisym.Symbols["github.com/spf13/pflag/pflag"],
	}
}

func Load(pluginDir string) (*LoadedPlugin, error) {
	info, err := LoadPluginInfo(pluginDir)
	if err != nil {
		return nil, fmt.Errorf("load plugin info: %w", err)
	}

	entryDir := filepath.Dir(filepath.Join(pluginDir, info.Entry))

	i, err := yaegibase.New(pluginExports())
	if err != nil {
		return nil, err
	}
	if err := i.Use(scriptfs.New(entryDir).Exports()); err != nil {
		return nil, fmt.Errorf("use plugin fs %s: %w", entryDir, err)
	}

	merged, err := gosrc.MergeGoSources(entryDir)
	if err != nil {
		return nil, fmt.Errorf("merge go files in %s: %w", entryDir, err)
	}

	if _, err := i.Eval(merged); err != nil {
		return nil, fmt.Errorf("yaegi eval plugin %s: %w", info.Name, err)
	}

	v, err := i.Eval("Main")
	if err != nil {
		return nil, fmt.Errorf("yaegi lookup Main in plugin %s: %w", info.Name, err)
	}
	mainFunc, ok := v.Interface().(func(*Context))
	if !ok {
		return nil, fmt.Errorf("plugin %s: Main has wrong signature: %T", info.Name, v.Interface())
	}

	return &LoadedPlugin{
		Info:  info,
		Entry: mainFunc,
	}, nil
}

func RunMain(loaded *LoadedPlugin, ctx *Context) {
	loaded.Entry(ctx)
}
