package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/plugin"
	"gitee.com/spock2300/vmake/pkg/repo"
)

type Source struct {
	ID        string
	BuildGo   string
	Dir       string
	OutputDir string
	Origin    api.SourceOrigin
	Force     bool
}

type PackageNode struct {
	ID       string
	Source   *Source
	Pkg      *api.Package
	Deps     []string
	Deferred bool
}

func (n *PackageNode) IsLocal() bool {
	return n.Source != nil && n.Source.Origin == api.SourceLocal
}

type Graph struct {
	Packages map[string]*PackageNode
	Order    []string
}

type Resolver struct {
	sources  map[string]*Source
	graph    *Graph
	repoMgr  *repo.RepoManager
	cacheDir string
	force    bool
}

func NewResolver(repoMgr *repo.RepoManager, cacheDir string) *Resolver {
	return &Resolver{
		sources:  make(map[string]*Source),
		graph:    &Graph{Packages: make(map[string]*PackageNode)},
		repoMgr:  repoMgr,
		cacheDir: cacheDir,
	}
}

func (r *Resolver) SetForce(force bool) {
	r.force = force
}

func (r *Resolver) Graph() *Graph {
	return r.graph
}

func (r *Resolver) GetOrder() []string {
	return r.graph.Order
}

func (r *Resolver) UpdateOrder() {
	r.graph.Order = topologicalSort(r.graph.Packages)
}

func (r *Resolver) ResolveAll(localSources []plugin.Source) error {
	for _, src := range localSources {
		s := &Source{
			ID:      src.Name,
			BuildGo: src.Path,
			Dir:     src.Dir,
			Origin:  api.SourceLocal,
			Force:   r.force,
		}
		r.sources[s.ID] = s
	}

	for _, src := range localSources {
		if _, exists := r.graph.Packages[src.Name]; exists {
			continue
		}
		if _, err := r.resolveRecursive(src.Name, nil); err != nil {
			return err
		}
	}

	r.UpdateOrder()
	return nil
}

func (r *Resolver) ResolveDeferred() error {
	for {
		r.UpdateOrder()
		hasDeferred := false
		for _, id := range r.graph.Order {
			node := r.graph.Packages[id]
			if node == nil || !node.Deferred {
				continue
			}
			hasDeferred = true
			newNode, err := r.resolveDeferredNode(id, node)
			if err != nil {
				return err
			}
			r.graph.Packages[id] = newNode
		}
		if !hasDeferred {
			break
		}
	}
	r.UpdateOrder()
	return nil
}

func (r *Resolver) resolveDeferredNode(id string, node *PackageNode) (*PackageNode, error) {
	src := node.Source
	if src == nil {
		return nil, fmt.Errorf("deferred node %s has no source", id)
	}

	pluginPath := r.pluginPath(src)
	if !r.force && src.BuildGo != "" && r.hasCachedPlugin(pluginPath, src.BuildGo) {
		return r.resolveFromCache(id, pluginPath, src, []string{id})
	}

	return r.resolveOne(id, src, []string{id})
}

func (r *Resolver) FilterDeps(id string, cfgVals map[string]any, options map[string]*api.Option) error {
	node, exists := r.graph.Packages[id]
	if !exists {
		return fmt.Errorf("package %s not in graph", id)
	}
	if node.Pkg == nil {
		return nil
	}

	pkg := node.Pkg
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

func (r *Resolver) resolveRecursive(id string, path []string) (*PackageNode, error) {
	for _, p := range path {
		if p == id {
			return nil, fmt.Errorf("circular dependency: %s → %s",
				strings.Join(path, " → "), id)
		}
	}

	if node, exists := r.graph.Packages[id]; exists {
		return node, nil
	}

	src, err := r.findSource(id)
	if err != nil {
		return nil, err
	}

	pluginPath := r.pluginPath(src)
	if !r.force && src.BuildGo != "" && r.hasCachedPlugin(pluginPath, src.BuildGo) {
		return r.resolveFromCache(id, pluginPath, src, path)
	}

	if src.Origin == api.SourceRemote {
		node := &PackageNode{
			ID:       id,
			Source:   src,
			Deferred: true,
			Deps:     []string{},
		}
		r.graph.Packages[id] = node
		return node, nil
	}

	return r.resolveOne(id, src, path)
}

func (r *Resolver) resolveOne(id string, src *Source, path []string) (*PackageNode, error) {
	pluginPath := r.pluginPath(src)

	cr := plugin.Compile(plugin.Source{
		Name:      src.ID,
		Path:      src.BuildGo,
		Dir:       src.Dir,
		OutputDir: src.OutputDir,
		Origin:    src.Origin,
		Force:     src.Force,
	})
	if !cr.Success {
		return nil, fmt.Errorf("compile %s: %w", id, cr.Error)
	}

	return r.resolveFromCache(id, pluginPath, src, path)
}

func (r *Resolver) resolveFromCache(id string, pluginPath string, src *Source, path []string) (*PackageNode, error) {
	loaded, err := plugin.Load(pluginPath, plugin.Source{
		Name:      src.ID,
		Path:      src.BuildGo,
		Dir:       src.Dir,
		OutputDir: src.OutputDir,
		Origin:    src.Origin,
	})
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", id, err)
	}

	pkg := loaded.ExtractPackage()

	node := &PackageNode{
		ID:     id,
		Source: src,
		Pkg:    pkg,
		Deps:   []string{},
	}
	r.graph.Packages[id] = node

	for _, req := range pkg.GetRequireContext().GetRequires() {
		depNode, err := r.resolveRecursive(req.Name, append(path, id))
		if err != nil {
			return nil, err
		}
		node.Deps = append(node.Deps, depNode.ID)
	}

	return node, nil
}

func (r *Resolver) findSource(id string) (*Source, error) {
	if src, ok := r.sources[id]; ok {
		return src, nil
	}

	repoName, pkgName := parseRepoPkg(id)
	buildGo, err := r.repoMgr.FindPackageGo(repoName, pkgName)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", id, err)
	}

	src := &Source{
		ID:        id,
		BuildGo:   buildGo,
		Dir:       filepath.Dir(buildGo),
		OutputDir: r.pluginOutputDir(id),
		Origin:    api.SourceRemote,
		Force:     r.force,
	}
	r.sources[id] = src
	return src, nil
}

func (r *Resolver) pluginPath(src *Source) string {
	outputDir := src.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(src.Dir, "build")
	}
	return filepath.Join(outputDir, "plugin.so")
}

func (r *Resolver) pluginOutputDir(name string) string {
	return fmt.Sprintf("%s/plugins/%s", r.cacheDir, strings.ReplaceAll(name, "/", "_"))
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

func topologicalSort(packages map[string]*PackageNode) []string {
	inDegree := make(map[string]int)
	for name := range packages {
		inDegree[name] = 0
	}

	for name, pkg := range packages {
		for _, dep := range pkg.Deps {
			if _, exists := packages[dep]; exists {
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

		for name, pkg := range packages {
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

func parseRepoPkg(fullName string) (string, string) {
	lastSlash := strings.LastIndex(fullName, "/")
	if lastSlash < 0 {
		return "", fullName
	}
	return fullName[:lastSlash], fullName[lastSlash+1:]
}
