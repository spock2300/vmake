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
	cr := plugin.Compile(src)
	if !cr.Success {
		return fmt.Errorf("compile %s failed: %w", name, cr.Error)
	}

	loaded, err := plugin.Load(cr.PluginPath, src)
	if err != nil {
		return fmt.Errorf("load %s failed: %w", name, err)
	}

	pkg := loaded.ExtractPackage()

	node := &ResolvedPackage{
		Name:       name,
		Definition: pkg,
		Source:     &src,
		Deps:       []string{},
	}

	requires := pkg.GetRequireContext().GetRequires()
	for _, req := range requires {
		if err := r.resolveRecursive(req, graph, append(path, name)); err != nil {
			return err
		}
		node.Deps = append(node.Deps, req.Name)
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

	src := plugin.Source{
		Name:      name,
		OutputDir: pluginDir,
		Origin:    plugin.SourceRemote,
	}

	if r.hasCachedPlugin(pluginPath) {
		loaded, err := plugin.Load(pluginPath, src)
		if err != nil {
			return fmt.Errorf("load cached %s failed: %w", name, err)
		}

		pkgDef := loaded.ExtractPackage()
		var subDeps []string

		requireCtx := pkgDef.GetRequireContext()
		for _, dep := range requireCtx.GetRequires() {
			if err := r.resolveRecursive(dep, graph, append(path, name)); err != nil {
				return err
			}
			subDeps = append(subDeps, dep.Name)
		}

		graph.Packages[name] = &ResolvedPackage{
			Name:       name,
			Constraint: req.Constraint,
			Options:    make(map[string]any),
			Definition: pkgDef,
			Source:     &src,
			Deps:       subDeps,
		}
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

func (r *Resolver) hasCachedPlugin(pluginPath string) bool {
	info, err := os.Stat(pluginPath)
	return err == nil && info.Size() > 0
}

func (r *Resolver) ResolveSingle(name string, graph *DependencyGraph) error {
	node, exists := graph.Packages[name]
	if !exists {
		return fmt.Errorf("package %s not in dependency graph", name)
	}
	if !node.Deferred {
		return nil
	}

	pkgPath, err := r.repoMgr.FindPackageGo(r.parseRepo(name), r.parsePkgName(name))
	if err != nil {
		return fmt.Errorf("failed to find package %s: %w", name, err)
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
		return fmt.Errorf("compile %s failed: %w", name, cr.Error)
	}

	loaded, err := plugin.Load(cr.PluginPath, src)
	if err != nil {
		return fmt.Errorf("load %s failed: %w", name, err)
	}

	pkgDef := loaded.ExtractPackage()
	var subDeps []string

	requireCtx := pkgDef.GetRequireContext()
	for _, dep := range requireCtx.GetRequires() {
		if err := r.resolveRecursive(dep, graph, nil); err != nil {
			return err
		}
		subDeps = append(subDeps, dep.Name)
	}

	node.Definition = pkgDef
	node.Source = &src
	node.Deps = subDeps
	node.Deferred = false
	return nil
}

func (r *Resolver) pluginOutputDir(name string) string {
	return fmt.Sprintf("%s/plugins/%s", r.cacheDir, strings.ReplaceAll(name, "/", "_"))
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

func (r *Resolver) parseRepo(fullName string) string {
	parts := strings.Split(fullName, "/")
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}

func (r *Resolver) parsePkgName(fullName string) string {
	parts := strings.Split(fullName, "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return fullName
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

	pkgPath, err := r.repoMgr.FindPackageGo(r.parseRepo(name), r.parsePkgName(name))
	if err != nil {
		return fmt.Errorf("failed to find package %s: %w", name, err)
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
		return fmt.Errorf("compile %s failed: %w", name, cr.Error)
	}

	loaded, err := plugin.Load(cr.PluginPath, src)
	if err != nil {
		return fmt.Errorf("load %s failed: %w", name, err)
	}

	pkgDef := loaded.ExtractPackage()

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
	pkgPath, err := r.repoMgr.FindPackageGo(r.parseRepo(name), r.parsePkgName(name))
	if err != nil {
		return nil, fmt.Errorf("failed to find package %s: %w", name, err)
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
		return nil, fmt.Errorf("compile %s failed: %w", name, cr.Error)
	}

	loaded, err := plugin.Load(cr.PluginPath, src)
	if err != nil {
		return nil, fmt.Errorf("load %s failed: %w", name, err)
	}

	return loaded.ExtractPackage(), nil
}
