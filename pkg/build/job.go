package build

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type BuildEnv struct {
	Layouts       map[string]*PackageLayout
	Packages      map[string]*api.Package
	WorkspaceDirs map[string]string
	Toolchains    map[string]*toolchain.Toolchain
	Options       map[string]map[string]any
	GlobalFlags   [4][]string
	Session       *Session
	RootDir       string
	Jobs          int
	KeepGoing     bool
	SubParents    map[string]string
}

type BuildJob struct {
	Env          *BuildEnv
	Graph        *BuildGraph
	Toolchain    *toolchain.Toolchain
	Platform     api.Platform
	Mode         string
	IncludeTests bool
	Explain      bool
}

type buildGroupKey struct {
	Toolchain toolchain.Toolchain
	Platform  api.Platform
}

func (j *BuildJob) newSchedulerFor(
	env *BuildEnv,
	session *Session,
	tc *toolchain.Toolchain,
	platform api.Platform,
	tools *ResolvedTools,
	stats *BuildStats,
) *Scheduler {
	return newScheduler(schedulerConfig{
		Graph:         j.Graph,
		Toolchain:     tc,
		Platform:      platform,
		Layouts:       env.Layouts,
		Mode:          j.Mode,
		Options:       env.Options,
		Packages:      env.Packages,
		WorkspaceDirs: env.WorkspaceDirs,
		Tools:         tools,
		Context:       session.ctx,
		RootDir:       env.RootDir,
		IncludeTests:  j.IncludeTests,
		NumWorkers:    env.Jobs,
		KeepGoing:     env.KeepGoing,
		Explain:       j.Explain,
		GlobalFlags:   &env.GlobalFlags,
		Stats:         stats,
		Session:       session,
	})
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
	stats := session.Stats()
	base := j.newSchedulerFor(env, session, j.Toolchain, defaultPlatform, defaultTools, stats)
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
	names := make([]string, 0, len(graphPackages))
	for name := range graphPackages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tc := env.Toolchains[name]
		if tc == nil {
			tc = j.Toolchain
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
			runner = j.newSchedulerFor(env, session, tc, platform, tools, stats)
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
		return session.Execute(name, func() error {
			return runner.Build(name)
		})
	}
	err = base.BuildAll()
	root := env.RootDir
	if root == "" {
		root = "."
	}
	for _, scheduler := range additional {
		err = errors.Join(err, scheduler.ccWriter.Save(filepath.Join(root, "build", "compile_commands.json")))
	}
	return base, err
}
