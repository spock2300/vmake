package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/plugin"
)

type DependencyGraph struct {
	Order    []string
	Packages map[string]*ResolvedPackage
}

type ResolvedPackage struct {
	Name       string
	Constraint string
	Options    map[string]any
	Definition *api.Package
	Source     *plugin.Source
	Deps       []string
	Deferred   bool
}

func (p *ResolvedPackage) IsLocal() bool {
	return p.Source != nil && p.Source.Origin == plugin.SourceLocal
}

type PackageRegistry struct {
	Definitions map[string]*api.Package
	Order       []string
}

type Resolver struct {
	repoMgr  *RepoManager
	cacheDir string
}

func NewResolver(repoMgr *RepoManager, cacheDir string) *Resolver {
	return &Resolver{repoMgr: repoMgr, cacheDir: cacheDir}
}

func (r *Resolver) Resolve(initial []api.RequireInfo) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Order:    []string{},
		Packages: make(map[string]*ResolvedPackage),
	}

	for _, req := range initial {
		if err := r.resolveRecursive(req, graph, nil); err != nil {
			return nil, err
		}
	}

	graph.Order = r.topologicalSort(graph)
	return graph, nil
}

func (r *Resolver) ResolveWithLocal(localPkgs []plugin.Source, force bool) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Order:    []string{},
		Packages: make(map[string]*ResolvedPackage),
	}

	for _, src := range localPkgs {
		if err := r.resolveLocal(src, graph, nil, force); err != nil {
			return nil, err
		}
	}

	graph.Order = r.topologicalSort(graph)
	return graph, nil
}

func (r *Resolver) resolveLocal(src plugin.Source, graph *DependencyGraph, path []string, force bool) error {
	name := src.Name

	for _, p := range path {
		if p == name {
			return fmt.Errorf("circular dependency: %s → %s", strings.Join(path, " → "), name)
		}
	}

	if _, exists := graph.Packages[name]; exists {
		return nil
	}

	src.Force = force

	pluginPath := pluginCachePath(src)
	if !force && src.Path != "" && r.hasCachedPlugin(pluginPath, src.Path) {
		node, err := r.resolveFromPlugin(name, pluginPath, src, graph, path)
		if err != nil {
			return err
		}
		graph.Packages[name] = node
		return nil
	}

	cr := plugin.Compile(src)
	if !cr.Success {
		return fmt.Errorf("compile %s failed: %w", name, cr.Error)
	}

	node, err := r.resolveFromPlugin(name, cr.PluginPath, src, graph, path)
	if err != nil {
		return err
	}
	graph.Packages[name] = node
	return nil
}

func (r *Resolver) resolveRecursive(req api.RequireInfo, graph *DependencyGraph, path []string) error {
	name := req.Name

	for _, p := range path {
		if p == name {
			return fmt.Errorf("circular dependency: %s → ... → %s",
				strings.Join(path, " → "), name)
		}
	}

	if _, exists := graph.Packages[name]; exists {
		return nil
	}

	pluginDir := r.pluginOutputDir(name)
	pluginPath := filepath.Join(pluginDir, "plugin.so")

	pkgPath, findErr := r.repoMgr.FindPackageGo(ParseRepo(name), ParsePkgName(name))

	src := plugin.Source{
		Name:      name,
		OutputDir: pluginDir,
		Origin:    plugin.SourceRemote,
	}

	if findErr == nil && r.hasCachedPlugin(pluginPath, pkgPath) {
		src.Path = pkgPath
		src.Dir = filepath.Dir(pkgPath)
		node, err := r.resolveFromPlugin(name, pluginPath, src, graph, path)
		if err != nil {
			return err
		}
		node.Constraint = req.Constraint
		node.Options = make(map[string]any)
		graph.Packages[name] = node
		return nil
	}

	graph.Packages[name] = &ResolvedPackage{
		Name:       name,
		Constraint: req.Constraint,
		Options:    make(map[string]any),
		Source:     &src,
		Deferred:   true,
	}
	return nil
}

func (r *Resolver) resolveFromPlugin(name, pluginPath string, src plugin.Source, graph *DependencyGraph, path []string) (*ResolvedPackage, error) {
	loaded, err := plugin.Load(pluginPath, src)
	if err != nil {
		return nil, fmt.Errorf("load %s failed: %w", name, err)
	}

	pkg := loaded.ExtractPackage()

	node := &ResolvedPackage{
		Name:       name,
		Definition: pkg,
		Source:     &src,
		Deps:       []string{},
	}

	for _, req := range pkg.GetRequireContext().GetRequires() {
		if err := r.resolveRecursive(req, graph, append(path, name)); err != nil {
			return nil, err
		}
		node.Deps = append(node.Deps, req.Name)
	}

	return node, nil
}

func (r *Resolver) hasCachedPlugin(pluginPath, buildGoPath string) bool {
	info, err := os.Stat(pluginPath)
	if err != nil || info.Size() == 0 {
		return false
	}
	if exe, err := os.Executable(); err == nil {
		if exeStat, err := os.Stat(exe); err == nil {
			if exeStat.ModTime().After(info.ModTime()) {
				return false
			}
		}
	}
	if buildGoPath != "" {
		if src, err := os.Stat(buildGoPath); err == nil {
			if src.ModTime().After(info.ModTime()) {
				return false
			}
		}
	}
	return true
}

func pluginCachePath(src plugin.Source) string {
	outputDir := src.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(src.Dir, "build")
	}
	return filepath.Join(outputDir, "plugin.so")
}

func (r *Resolver) ResolveSingle(name string, graph *DependencyGraph) error {
	node, exists := graph.Packages[name]
	if !exists {
		return fmt.Errorf("package %s not in dependency graph", name)
	}
	if !node.Deferred {
		return nil
	}

	pkg, src, err := r.loadRemotePackage(name)
	if err != nil {
		return err
	}

	node.Definition = pkg
	node.Source = &src
	node.Deps = []string{}
	for _, req := range pkg.GetRequireContext().GetRequires() {
		node.Deps = append(node.Deps, req.Name)
	}
	node.Deferred = false
	return nil
}

// FilterDeps re-runs OnRequire with actual config values and updates node.Deps.
func (r *Resolver) FilterDeps(name string, graph *DependencyGraph, cfgVals map[string]any, options map[string]*api.Option) error {
	node, exists := graph.Packages[name]
	if !exists {
		return fmt.Errorf("package %s not in dependency graph", name)
	}
	if node.Definition == nil {
		return nil
	}

	pkg := node.Definition
	requireFuncs := pkg.GetRequireFuncs()
	if len(requireFuncs) == 0 {
		return nil
	}

	pkg.UpdateRequireContext(cfgVals, options)

	deps := pkg.GetRequireContext().GetRequires()
	newDeps := make([]string, 0, len(deps))
	for _, req := range deps {
		newDeps = append(newDeps, req.Name)
	}
	node.Deps = newDeps
	return nil
}

func (r *Resolver) pluginOutputDir(name string) string {
	return fmt.Sprintf("%s/plugins/%s", r.cacheDir, strings.ReplaceAll(name, "/", "_"))
}

func (r *Resolver) loadRemotePackage(name string) (*api.Package, plugin.Source, error) {
	pkgPath, err := r.repoMgr.FindPackageGo(ParseRepo(name), ParsePkgName(name))
	if err != nil {
		return nil, plugin.Source{}, fmt.Errorf("failed to find package %s: %w", name, err)
	}

	src := plugin.Source{
		Name:      name,
		Path:      pkgPath,
		Dir:       filepath.Dir(pkgPath),
		OutputDir: r.pluginOutputDir(name),
		Origin:    plugin.SourceRemote,
	}

	cr := plugin.Compile(src)
	if !cr.Success {
		return nil, src, fmt.Errorf("compile %s failed: %w", name, cr.Error)
	}

	loaded, err := plugin.Load(cr.PluginPath, src)
	if err != nil {
		return nil, src, fmt.Errorf("load %s failed: %w", name, err)
	}

	return loaded.ExtractPackage(), src, nil
}

func (r *Resolver) topologicalSort(graph *DependencyGraph) []string {
	inDegree := make(map[string]int)
	for name := range graph.Packages {
		inDegree[name] = 0
	}

	for name, pkg := range graph.Packages {
		for _, dep := range pkg.Deps {
			if _, exists := graph.Packages[dep]; exists {
				inDegree[name]++
			}
		}
	}

	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	sort.Strings(queue)

	var result []string
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		for name, pkg := range graph.Packages {
			for _, dep := range pkg.Deps {
				if dep == current {
					inDegree[name]--
					if inDegree[name] == 0 {
						queue = append(queue, name)
						sort.Strings(queue)
					}
				}
			}
		}
	}

	return result
}

func (r *Resolver) CollectDeclarations(initial []api.RequireInfo) (*PackageRegistry, error) {
	registry := &PackageRegistry{
		Definitions: make(map[string]*api.Package),
		Order:       []string{},
	}

	for _, req := range initial {
		if err := r.collectRecursive(req.Name, registry, nil); err != nil {
			return nil, err
		}
	}

	return registry, nil
}

func (r *Resolver) collectRecursive(name string, registry *PackageRegistry, path []string) error {
	for _, p := range path {
		if p == name {
			return fmt.Errorf("circular declaration: %s → ... → %s",
				strings.Join(path, " → "), name)
		}
	}

	if _, exists := registry.Definitions[name]; exists {
		return nil
	}

	pkgDef, _, err := r.loadRemotePackage(name)
	if err != nil {
		return err
	}

	for _, declared := range pkgDef.GetDeclaredPackages() {
		if err := r.collectRecursive(declared, registry, append(path, name)); err != nil {
			return err
		}
	}

	registry.Definitions[name] = pkgDef
	registry.Order = append(registry.Order, name)
	return nil
}

func (r *Resolver) LoadDefinition(name string) (*api.Package, error) {
	pkg, _, err := r.loadRemotePackage(name)
	if err != nil {
		return nil, err
	}
	return pkg, nil
}
