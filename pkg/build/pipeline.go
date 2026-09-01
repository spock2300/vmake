package build

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type BuildPipeline struct {
	Graph        *BuildGraph
	Toolchain    *toolchain.Toolchain
	PkgDirs      map[string]*api.PkgDirs
	Mode         string
	Options      map[string]map[string]any
	Packages     map[string]*api.Package
	RootDir      string
	IncludeTests bool
	PkgKeyExtra  map[string]string
	PkgLockDir   string
	NumWorkers   int
	ParallelPkgs int
	KeepGoing    bool
}

func NewBuildPipeline(graph *BuildGraph, tc *toolchain.Toolchain, pkgDirs map[string]*api.PkgDirs, mode string, options map[string]map[string]any) *BuildPipeline {
	return &BuildPipeline{
		Graph:     graph,
		Toolchain: tc,
		PkgDirs:   pkgDirs,
		Mode:      mode,
		Options:   options,
		Packages:  make(map[string]*api.Package),
	}
}

func (p *BuildPipeline) SetPackage(pkgName string, pkg *api.Package) {
	p.Packages[pkgName] = pkg
}

func (p *BuildPipeline) SetRootDir(dir string) {
	p.RootDir = dir
}

func (p *BuildPipeline) SetIncludeTests(v bool) {
	p.IncludeTests = v
}

func (p *BuildPipeline) SetPkgKeyExtra(extra map[string]string) {
	p.PkgKeyExtra = extra
}

func (p *BuildPipeline) SetPkgLockDir(dir string) {
	p.PkgLockDir = dir
}

func (p *BuildPipeline) SetNumWorkers(n int) {
	p.NumWorkers = n
}

func (p *BuildPipeline) SetParallelPkgs(n int) {
	p.ParallelPkgs = n
}

func (p *BuildPipeline) SetKeepGoing(v bool) {
	p.KeepGoing = v
}

func (p *BuildPipeline) Run() (*Scheduler, error) {
	scheduler, err := NewScheduler(p.Graph, p.Toolchain, p.PkgDirs, p.Mode, p.Options)
	if err != nil {
		return nil, err
	}
	if p.RootDir != "" {
		scheduler.SetRootDir(p.RootDir)
	}
	scheduler.SetIncludeTests(p.IncludeTests)
	scheduler.SetPkgKeyExtra(p.PkgKeyExtra)
	scheduler.SetPkgLockDir(p.PkgLockDir)
	scheduler.SetNumWorkers(p.NumWorkers)
	parallelPkgs := p.ParallelPkgs
	if parallelPkgs == 0 {
		parallelPkgs = runtime.NumCPU()
	}
	scheduler.SetParallelPkgs(parallelPkgs)
	scheduler.SetKeepGoing(p.KeepGoing)

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
