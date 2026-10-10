package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type BuildPipeline struct {
	PackageToolchains map[string]*toolchain.Toolchain
	GlobalFlags       *[4][]string
	Graph             *BuildGraph
	Toolchain         *toolchain.Toolchain
	Platform          api.Platform
	PkgDirs           map[string]*api.PkgDirs
	Mode              string
	Options           map[string]map[string]any
	Packages          map[string]*api.Package
	RootDir           string
	IncludeTests      bool
	PkgKeyExtra       map[string]string
	NumWorkers        int
	Session           *Session
	KeepGoing         bool
}

func NewBuildPipeline(graph *BuildGraph, tc *toolchain.Toolchain, pkgDirs map[string]*api.PkgDirs, mode string, options map[string]map[string]any, platform api.Platform) *BuildPipeline {
	return &BuildPipeline{
		Platform:  platform,
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

func (p *BuildPipeline) SetNumWorkers(n int) {
	p.NumWorkers = n
}

func (p *BuildPipeline) SetKeepGoing(v bool) {
	p.KeepGoing = v
}

func (p *BuildPipeline) newScheduler(tc *toolchain.Toolchain, platform api.Platform) (*Scheduler, error) {
	tools, err := p.Session.ResolveTools(tc, platform)
	if err != nil {
		return nil, err
	}
	scheduler := newScheduler(p.Graph, tc, p.PkgDirs, p.Mode, p.Options, platform, tools)
	scheduler.ctx = p.Session.ctx
	scheduler.linker.run = gnuRunnerContext(p.Session.ctx, tools.env)
	scheduler.SetRootDir(p.RootDir)
	scheduler.SetIncludeTests(p.IncludeTests)
	scheduler.SetPkgKeyExtra(p.PkgKeyExtra)
	scheduler.SetNumWorkers(p.NumWorkers)
	scheduler.SetKeepGoing(p.KeepGoing)
	if p.GlobalFlags != nil {
		scheduler.globalCFlags = append([]string{}, p.GlobalFlags[0]...)
		scheduler.globalCxxFlags = append([]string{}, p.GlobalFlags[1]...)
		scheduler.globalLdFlags = append([]string{}, p.GlobalFlags[2]...)
		scheduler.globalLinks = append([]string{}, p.GlobalFlags[3]...)
	}
	for name, pkg := range p.Packages {
		scheduler.SetPackage(name, pkg)
	}
	return scheduler, nil
}

func (p *BuildPipeline) Run() (*Scheduler, error) {
	if p.Session == nil {
		p.Session = NewSession(nil)
		defer func() { p.Session = nil }()
	}
	scheduler, err := p.newScheduler(p.Toolchain, p.Platform)
	if err != nil {
		return nil, err
	}
	groups := map[string]*Scheduler{}
	runners := map[string]*Scheduler{}
	var additional []*Scheduler
	names := make([]string, 0, len(p.PackageToolchains))
	for name := range p.PackageToolchains {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		tc := p.PackageToolchains[name]
		platform := p.Platform
		if pkg := p.Packages[name]; pkg != nil {
			platform = api.Platform{OS: pkg.TargetOS(), Triple: pkg.TargetTriple()}
		}
		if *tc == *p.Toolchain && platform == p.Platform {
			continue
		}
		data, _ := json.Marshal(struct {
			Toolchain *toolchain.Toolchain
			Platform  api.Platform
		}{tc, platform})
		key := string(data)
		runner := groups[key]
		if runner == nil {
			runner, err = p.newScheduler(tc, platform)
			if err != nil {
				return nil, err
			}
			groups[key] = runner
			additional = append(additional, runner)
		}
		runners[name] = runner
		scheduler.pkgs[name] = runner.pkgs[name]
	}
	for _, runner := range additional {
		runner.pkgs = scheduler.pkgs
	}
	scheduler.SetBuildTargetFunc(func(name string) error {
		run := func() error {
			node := p.Graph.Nodes[name]
			runner := runners[node.PkgName]
			if runner == nil {
				runner = scheduler
			}
			if err := runner.Build(name); err != nil {
				return err
			}
			if p.Session == nil || !node.Target.IsDefault() || (node.Target.IsTest() && !p.IncludeTests) {
				return nil
			}
			output := runner.getTargetOutputPath(node)
			source := runner.pkgs[node.PkgName].SourceDir
			paths := append([]string{}, postLinkOutputPaths(node.Target, source, output)...)
			if node.Target.Kind() != api.TargetVoid && node.Target.Kind() != api.TargetObject {
				paths = append([]string{resolveWorkPath(source, output)}, paths...)
			}
			if p.Session.outputs == nil {
				p.Session.outputs = make(map[string]TargetResult)
			}
			p.Session.outputs[name] = TargetResult{Package: node.PkgName, Target: node.Target.Name(), Kind: string(node.Target.Kind()), Outputs: paths}
			return nil
		}
		if p.Session != nil {
			return p.Session.Execute(name, run)
		}
		return run()
	})
	for _, fullName := range p.Graph.Order {
		node := p.Graph.Nodes[fullName]
		if info, _ := scheduler.GetPkgInfo(node.PkgName); info != nil && info.InstallDir != "" {
			if err := os.MkdirAll(info.InstallDir, 0755); err != nil {
				return nil, fmt.Errorf("create install dir: %w", err)
			}
		}
	}
	err = scheduler.BuildAll()
	root := p.RootDir
	if root == "" {
		root = "."
	}
	for _, runner := range additional {
		err = errors.Join(err, runner.ccWriter.Save(filepath.Join(root, "build", "compile_commands.json")))
	}
	if err != nil {
		return nil, err
	}
	return scheduler, nil
}
