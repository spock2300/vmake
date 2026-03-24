package buildscript

import (
	"fmt"
	"plugin"

	"gitee.com/spock2300/vmake/pkg/api"
)

type LoadedScript struct {
	Source Source
	Script *plugin.Plugin
	pkg    *api.Package
}

func Load(scriptPath string, src Source) (*LoadedScript, error) {
	p, err := GlobalScript.Open(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open buildscript: %w", err)
	}

	return &LoadedScript{
		Source: src,
		Script: p,
	}, nil
}

func (l *LoadedScript) ExtractPackage() *api.Package {
	if l.pkg == nil {
		l.pkg = ExtractPackage(l)
	}
	return l.pkg
}

func (l *LoadedScript) GetRequires() []api.RequireInfo {
	pkg := l.ExtractPackage()
	return pkg.GetRequireContext().GetRequires()
}
