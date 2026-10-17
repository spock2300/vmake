package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type BuildEnv struct {
	Layouts     map[string]*PackageLayout
	Packages    map[string]*api.Package
	Toolchains  map[string]*toolchain.Toolchain
	Options     map[string]map[string]any
	GlobalFlags [4][]string
	Session     *Session
	RootDir     string
	Jobs        int
	KeepGoing   bool
	SubParents  map[string]string
}

type BuildJob struct {
	Env          *BuildEnv
	Graph        *BuildGraph
	Toolchain    *toolchain.Toolchain
	Platform     api.Platform
	Mode         string
	IncludeTests bool
}

type buildGroupKey struct {
	Toolchain toolchain.Toolchain
	Platform  api.Platform
}

func (j *BuildJob) Run() (*Scheduler, error) {
	if j == nil || j.Graph == nil {
		return nil, fmt.Errorf("build job has no graph")
	}
	if j.Env == nil {
		return nil, fmt.Errorf("build job has no environment")
	}
	if j.Toolchain == nil {
		return nil, fmt.Errorf("build job has no toolchain")
	}
	env := j.Env
	if env.Layouts == nil {
		env.Layouts = make(map[string]*PackageLayout)
	}
	if env.Packages == nil {
		env.Packages = make(map[string]*api.Package)
	}
	if env.Options == nil {
		env.Options = make(map[string]map[string]any)
	}
	session := env.Session
	if session == nil {
		session = NewSession(nil)
		env.Session = session
	}
	defaultPlatform := j.Platform
	defaultPlatform.OS = defaultPlatform.OSOrHost()
	defaultTools, err := session.ResolveTools(j.Toolchain, defaultPlatform)
	if err != nil {
		return nil, err
	}
	base := newScheduler(schedulerConfig{
		Graph:        j.Graph,
		Toolchain:    j.Toolchain,
		Platform:     defaultPlatform,
		Layouts:      env.Layouts,
		Mode:         j.Mode,
		Options:      env.Options,
		Packages:     env.Packages,
		Tools:        defaultTools,
		Context:      session.ctx,
		RootDir:      env.RootDir,
		IncludeTests: j.IncludeTests,
		NumWorkers:   env.Jobs,
		KeepGoing:    env.KeepGoing,
		GlobalFlags:  &env.GlobalFlags,
	})
	graphPackages := make(map[string]bool)
	for _, node := range j.Graph.Nodes {
		graphPackages[node.PkgName] = true
	}
	for name := range graphPackages {
		layout := env.Layouts[name]
		if layout == nil || !layout.Ready {
			return nil, fmt.Errorf("package %s has no ready build layout", name)
		}
		if tc, ok := env.Toolchains[name]; ok && tc == nil {
			return nil, fmt.Errorf("package %s has no toolchain", name)
		}
	}
	baseKey := buildGroupKey{Toolchain: *j.Toolchain, Platform: defaultPlatform}
	groups := make(map[buildGroupKey]*Scheduler)
	var additional []*Scheduler
	runners := make(map[string]*Scheduler)
	names := make([]string, 0, len(env.Toolchains))
	for name := range env.Toolchains {
		if graphPackages[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		tc := env.Toolchains[name]
		if tc == nil {
			continue
		}
		platform := defaultPlatform
		if pkg := env.Packages[name]; pkg != nil {
			platform = api.Platform{OS: pkg.TargetOS(), Triple: pkg.TargetTriple()}
		}
		platform.OS = platform.OSOrHost()
		key := buildGroupKey{Toolchain: *tc, Platform: platform}
		if key == baseKey {
			runners[name] = base
			continue
		}
		runner := groups[key]
		if runner == nil {
			tools, err := session.ResolveTools(tc, platform)
			if err != nil {
				return nil, err
			}
			runner = newScheduler(schedulerConfig{
				Graph:        j.Graph,
				Toolchain:    tc,
				Platform:     platform,
				Layouts:      env.Layouts,
				Mode:         j.Mode,
				Options:      env.Options,
				Packages:     env.Packages,
				Tools:        tools,
				Context:      session.ctx,
				RootDir:      env.RootDir,
				IncludeTests: j.IncludeTests,
				NumWorkers:   env.Jobs,
				KeepGoing:    env.KeepGoing,
				GlobalFlags:  &env.GlobalFlags,
			})
			groups[key] = runner
			additional = append(additional, runner)
		}
		runners[name] = runner
	}
	base.buildTargetFn = func(name string) error {
		node := j.Graph.Nodes[name]
		if node == nil {
			return fmt.Errorf("target not found: %s", name)
		}
		runner := runners[node.PkgName]
		if runner == nil {
			runner = base
		}
		run := func() error {
			if err := runner.Build(name); err != nil {
				return err
			}
			if !node.Target.IsDefault() || (node.Target.IsTest() && !j.IncludeTests) {
				return nil
			}
			output := runner.getTargetOutputPath(node)
			info := runner.pkgs[node.PkgName]
			if info == nil {
				return fmt.Errorf("package %s has no build layout", node.PkgName)
			}
			source := info.SourceDir
			paths := append([]string{}, postLinkOutputPaths(node.Target, source, output)...)
			if node.Target.Kind() != api.TargetVoid && node.Target.Kind() != api.TargetObject {
				paths = append([]string{resolveWorkPath(source, output)}, paths...)
			}
			if session.outputs == nil {
				session.outputs = make(map[string]TargetResult)
			}
			session.outputs[name] = TargetResult{Package: node.PkgName, Target: node.Target.Name(), Kind: string(node.Target.Kind()), Outputs: paths}
			return nil
		}
		return session.Execute(name, run)
	}
	for _, fullName := range j.Graph.Order {
		node := j.Graph.Nodes[fullName]
		if info, _ := base.GetPkgInfo(node.PkgName); info != nil && info.InstallDir != "" {
			if err := os.MkdirAll(info.InstallDir, 0755); err != nil {
				return nil, fmt.Errorf("create install dir: %w", err)
			}
		}
	}
	err = base.BuildAll()
	root := env.RootDir
	if root == "" {
		root = "."
	}
	for _, scheduler := range additional {
		err = errors.Join(err, scheduler.ccWriter.Save(filepath.Join(root, "build", "compile_commands.json")))
	}
	if err != nil {
		return nil, err
	}
	return base, nil
}
