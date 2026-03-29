package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/buildscript"
	"gitee.com/spock2300/vmake/pkg/repo"
)

type PackageNode struct {
	ID       string
	Source   *buildscript.Source
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
	sources  map[string]*buildscript.Source
	graph    *Graph
	repoMgr  *repo.RepoManager
	cacheDir string
	force    bool
}

func NewResolver(repoMgr *repo.RepoManager, cacheDir string) *Resolver {
	return &Resolver{
		sources:  make(map[string]*buildscript.Source),
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
	order, err := topologicalSort(r.graph.Packages)
	if err != nil {
		r.graph.Order = []string{}
		return
	}
	r.graph.Order = order
}

func (r *Resolver) ResolveAll(localSources []buildscript.Source) error {
	for _, src := range localSources {
		s := &buildscript.Source{
			Name:   src.Name,
			Path:   src.Path,
			Dir:    src.Dir,
			Origin: api.SourceLocal,
			Force:  r.force,
		}
		r.sources[s.Name] = s
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

	scriptPath := r.scriptPath(src)
	if !r.force && src.Path != "" && r.hasCachedScript(scriptPath, src.Path) {
		return r.resolveFromCache(id, scriptPath, src, []string{id})
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
	if err := api.CheckCycle(path, id); err != nil {
		return nil, err
	}

	if node, exists := r.graph.Packages[id]; exists {
		return node, nil
	}

	src, err := r.findSource(id)
	if err != nil {
		return nil, err
	}

	scriptPath := r.scriptPath(src)
	if !r.force && src.Path != "" && r.hasCachedScript(scriptPath, src.Path) {
		return r.resolveFromCache(id, scriptPath, src, path)
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

func (r *Resolver) resolveOne(id string, src *buildscript.Source, path []string) (*PackageNode, error) {
	scriptPath := r.scriptPath(src)

	cr := buildscript.Compile(*src)
	if !cr.Success {
		return nil, fmt.Errorf("compile %s: %w", id, cr.Error)
	}

	return r.resolveFromCache(id, scriptPath, src, path)
}

func (r *Resolver) resolveFromCache(id string, scriptPath string, src *buildscript.Source, path []string) (*PackageNode, error) {
	loaded, err := buildscript.Load(scriptPath, *src)
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

func (r *Resolver) findSource(id string) (*buildscript.Source, error) {
	if src, ok := r.sources[id]; ok {
		return src, nil
	}

	repoName, pkgName, _ := api.SplitPackageRef(id)
	buildGo, err := r.repoMgr.FindPackageGo(repoName, pkgName)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", id, err)
	}

	src := &buildscript.Source{
		Name:      id,
		Path:      buildGo,
		Dir:       filepath.Dir(buildGo),
		OutputDir: r.buildscriptOutputDir(id),
		Origin:    api.SourceRemote,
		Force:     r.force,
	}
	r.sources[id] = src
	return src, nil
}

func (r *Resolver) scriptPath(src *buildscript.Source) string {
	return filepath.Join(src.GetOutputDir(), "build.so")
}

func (r *Resolver) buildscriptOutputDir(name string) string {
	return fmt.Sprintf("%s/buildscripts/%s", r.cacheDir, strings.ReplaceAll(name, "/", "_"))
}

func (r *Resolver) hasCachedScript(scriptPath, buildGoPath string) bool {
	info, err := os.Stat(scriptPath)
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

func topologicalSort(packages map[string]*PackageNode) ([]string, error) {
	inDegree := make(map[string]int, len(packages))
	dependents := make(map[string][]string)
	for name := range packages {
		inDegree[name] = 0
	}

	for name, pkg := range packages {
		for _, dep := range pkg.Deps {
			if _, exists := packages[dep]; exists {
				inDegree[name]++
				dependents[dep] = append(dependents[dep], name)
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

	result := make([]string, 0, len(packages))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		for _, dep := range dependents[current] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
		sort.Strings(queue)
	}

	if len(result) != len(packages) {
		remaining := make([]string, 0)
		for name := range packages {
			if inDegree[name] > 0 {
				remaining = append(remaining, name)
			}
		}
		sort.Strings(remaining)
		return nil, fmt.Errorf("circular dependency detected involving: %s", strings.Join(remaining, ", "))
	}

	return result, nil
}
