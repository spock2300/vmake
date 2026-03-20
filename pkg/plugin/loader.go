package plugin

import (
	"fmt"
	"plugin"

	"gitee.com/spock2300/vmake/pkg/api"
)

type LoadedPlugin struct {
	Source Source
	Plugin *plugin.Plugin
	pkg    *api.Package
}

func Load(pluginPath string, src Source) (*LoadedPlugin, error) {
	p, err := GlobalManager.Open(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin: %w", err)
	}

	return &LoadedPlugin{
		Source: src,
		Plugin: p,
	}, nil
}

func (l *LoadedPlugin) ExtractPackage() *api.Package {
	if l.pkg == nil {
		l.pkg = ExtractPackage(l)
	}
	return l.pkg
}

func (l *LoadedPlugin) GetRequires() []api.RequireInfo {
	pkg := l.ExtractPackage()
	return pkg.GetRequireContext().GetRequires()
}
