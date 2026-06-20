package build

import (
	"fmt"
	"os"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type BuildPipeline struct {
	Graph             *BuildGraph
	Toolchain         *toolchain.Toolchain
	PkgDirs           map[string]*api.PkgDirs
	Mode              string
	Options           map[string]map[string]any
	Packages          map[string]*api.Package
	BuildKeyOverrides map[string]string
}

func NewBuildPipeline(graph *BuildGraph, tc *toolchain.Toolchain, pkgDirs map[string]*api.PkgDirs, mode string, options map[string]map[string]any, buildKeyOverrides map[string]string) *BuildPipeline {
	return &BuildPipeline{
		Graph:             graph,
		Toolchain:         tc,
		PkgDirs:           pkgDirs,
		Mode:              mode,
		Options:           options,
		Packages:          make(map[string]*api.Package),
		BuildKeyOverrides: buildKeyOverrides,
	}
}

func (p *BuildPipeline) SetPackage(pkgName string, pkg *api.Package) {
	p.Packages[pkgName] = pkg
}

func (p *BuildPipeline) Run() (*Scheduler, error) {
	scheduler, err := NewScheduler(p.Graph, p.Toolchain, p.PkgDirs, p.Mode, p.Options, p.BuildKeyOverrides)
	if err != nil {
		return nil, err
	}

	for name, pkg := range p.Packages {
		scheduler.SetPackage(name, pkg)
	}

	for _, fullName := range p.Graph.Order {
		node := p.Graph.Nodes[fullName]
		if info, _ := scheduler.GetPkgInfo(node.PkgName); info != nil && info.InstallDir != "" {
			if err := os.MkdirAll(info.InstallDir, 0755); err != nil {
				return nil, fmt.Errorf("create install dir: %w", err)
			}
		}
	}

	if err := scheduler.BuildAll(); err != nil {
		return nil, err
	}
	return scheduler, nil
}
