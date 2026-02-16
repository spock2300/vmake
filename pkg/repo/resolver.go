package repo

import (
	"fmt"
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
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
	Deps       []string
}

type PackageRegistry struct {
	Definitions map[string]*api.Package
	Order       []string
}

type Resolver struct {
	repoMgr   *RepoManager
	pkgLoader *PackageLoader
}

func NewResolver(repoMgr *RepoManager) *Resolver {
	return &Resolver{repoMgr: repoMgr}
}

func NewResolverWithLoader(repoMgr *RepoManager, loader *PackageLoader) *Resolver {
	return &Resolver{repoMgr: repoMgr, pkgLoader: loader}
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

	pkgPath, err := r.repoMgr.FindPackageGo(r.parseRepo(name), r.parsePkgName(name))
	if err != nil {
		return fmt.Errorf("failed to find package %s: %w", name, err)
	}

	var subDeps []string
	var pkgDef *api.Package

	if r.pkgLoader != nil {
		pkgDef, err = r.pkgLoader.Load(pkgPath)
		if err != nil {
			return fmt.Errorf("failed to load package %s: %w", name, err)
		}

		requireCtx := pkgDef.GetRequireContext()
		for _, dep := range requireCtx.GetRequires() {
			if err := r.resolveRecursive(dep, graph, append(path, name)); err != nil {
				return err
			}
			subDeps = append(subDeps, dep.Name)
		}
	}

	graph.Packages[name] = &ResolvedPackage{
		Name:       name,
		Constraint: req.Constraint,
		Options:    make(map[string]any),
		Definition: pkgDef,
		Deps:       subDeps,
	}

	return nil
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

	var pkgDef *api.Package
	if r.pkgLoader != nil {
		pkgDef, err = r.pkgLoader.Load(pkgPath)
		if err != nil {
			return fmt.Errorf("failed to load package %s: %w", name, err)
		}

		for _, declared := range pkgDef.GetDeclaredPackages() {
			if err := r.collectRecursive(declared, registry, append(path, name)); err != nil {
				return err
			}
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

	if r.pkgLoader == nil {
		return nil, fmt.Errorf("no package loader configured")
	}

	return r.pkgLoader.Load(pkgPath)
}
