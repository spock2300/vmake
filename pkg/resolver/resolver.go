package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/internal/toposort"
	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/buildscript"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/repo"
)

type NativePackageInfo struct {
	GitURL   string
	Versions map[string]string
	Selected string
}

type PackageNode struct {
	ID          string
	Source      *buildscript.Source
	Pkg         *api.Package
	Deps        []string
	Deferred    bool
	Native      *NativePackageInfo
	Constraints []string
}

func NewPackageNode(id string, src *buildscript.Source, pkg *api.Package, deferred bool) *PackageNode {
	return &PackageNode{
		ID:       id,
		Source:   src,
		Pkg:      pkg,
		Deferred: deferred,
		Deps:     []string{},
	}
}

func (n *PackageNode) WithNative(gitURL string, versions map[string]string, selected string) *PackageNode {
	n.Native = &NativePackageInfo{GitURL: gitURL, Versions: versions, Selected: selected}
	return n
}

func (n *PackageNode) IsLocal() bool {
	return n.Source != nil && n.Source.IsLocal()
}

func (n *PackageNode) IsNative() bool {
	return n.Native != nil
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

func (r *Resolver) UpdateOrder() error {
	order, err := topologicalSort(r.graph.Packages)
	if err != nil {
		return fmt.Errorf("dependency cycle detected: %w", err)
	}
	r.graph.Order = order
	return nil
}

func (r *Resolver) ResolveAll(localSources []buildscript.Source) error {
	for _, src := range localSources {
		s := buildscript.NewSource(src.Name, src.Path, src.Dir, "", api.SourceLocal, r.force)
		r.sources[s.Name] = s
	}

	for _, src := range localSources {
		if _, exists := r.graph.Packages[src.Name]; exists {
			continue
		}
		if _, err := r.resolveRecursive(src.Name, "", nil); err != nil {
			return err
		}
	}

	if err := r.UpdateOrder(); err != nil {
		return err
	}
	return nil
}

func (r *Resolver) ResolveDeferred() error {
	maxIter := len(r.graph.Packages) + 1
	for i := 0; i < maxIter; i++ {
		if err := r.UpdateOrder(); err != nil {
			return err
		}
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
	if err := r.UpdateOrder(); err != nil {
		return err
	}
	return nil
}

func (r *Resolver) resolveDeferredNode(id string, node *PackageNode) (*PackageNode, error) {
	src := node.Source
	if src == nil {
		return nil, fmt.Errorf("deferred node %s has no source", id)
	}

	newNode, err := r.resolvePackage(id, src, []string{id}, false)
	if err != nil {
		return nil, err
	}
	newNode.Native = node.Native
	newNode.Constraints = append([]string{}, node.Constraints...)

	if len(newNode.Constraints) > 0 && newNode.Pkg != nil && len(newNode.Pkg.Versions()) > 0 {
		if _, err := newNode.Pkg.SelectVersionMulti(newNode.Constraints); err != nil {
			return nil, fmt.Errorf("package %s: %w", id, err)
		}
	}

	return newNode, nil
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

	deps := pkg.GetRequires().Get()
	newDeps := make([]string, 0, len(deps))
	for _, req := range deps {
		newDeps = append(newDeps, req.Name)
	}
	node.Deps = newDeps
	return nil
}

func (r *Resolver) resolveRecursive(id string, constraint string, path []string) (*PackageNode, error) {
	if err := api.CheckCycle(path, id); err != nil {
		return nil, err
	}

	if node, exists := r.graph.Packages[id]; exists {
		if err := checkNodeConstraints(node, constraint); err != nil {
			return nil, err
		}
		return node, nil
	}

	src, err := r.findSource(id, constraint)
	if err != nil {
		return nil, err
	}

	if node, exists := r.graph.Packages[id]; exists {
		if err := checkNodeConstraints(node, constraint); err != nil {
			return nil, err
		}
		return node, nil
	}

	node, err := r.resolvePackage(id, src, path, true)
	if err != nil {
		return nil, err
	}
	if constraint != "" {
		node.Constraints = append(node.Constraints, constraint)
	}

	if len(node.Constraints) > 0 && node.Pkg != nil && len(node.Pkg.Versions()) > 0 {
		if _, err := node.Pkg.SelectVersionMulti(node.Constraints); err != nil {
			return nil, fmt.Errorf("package %s: %w", id, err)
		}
	}

	return node, nil
}

func (r *Resolver) resolvePackage(id string, src *buildscript.Source, path []string, allowDefer bool) (*PackageNode, error) {
	scriptPath := r.scriptPath(src)
	if !r.force && src.Path != "" && r.hasCachedScript(scriptPath, src.Path) {
		pkg, err := r.loadPackageFromCache(scriptPath, *src)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", id, err)
		}
		return r.resolveFromCache(id, pkg, src, path)
	}
	if allowDefer && src.IsRemote() {
		node := NewPackageNode(id, src, nil, true)
		r.graph.Packages[id] = node
		return node, nil
	}
	return r.resolveOne(id, src, path)
}

func (r *Resolver) resolveOne(id string, src *buildscript.Source, path []string) (*PackageNode, error) {
	pkg, err := r.PreparePackage(src)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", id, err)
	}

	return r.resolveFromCache(id, pkg, src, path)
}

func (r *Resolver) PreparePackage(src *buildscript.Source) (*api.Package, error) {
	scriptPath := r.scriptPath(src)

	if r.force {
		buildscript.GlobalScript.Invalidate(scriptPath)
	}

	cr := buildscript.Compile(*src)
	if !cr.Success {
		return nil, cr.Error
	}

	loaded, err := buildscript.Load(scriptPath, *src)
	if err != nil {
		return nil, err
	}

	return loaded.ExtractPackage(), nil
}

func (r *Resolver) loadPackageFromCache(scriptPath string, src buildscript.Source) (*api.Package, error) {
	loaded, err := buildscript.Load(scriptPath, src)
	if err != nil {
		return nil, err
	}
	return loaded.ExtractPackage(), nil
}

func (r *Resolver) resolveFromCache(id string, pkg *api.Package, src *buildscript.Source, path []string) (*PackageNode, error) {
	node := NewPackageNode(id, src, pkg, false)
	r.graph.Packages[id] = node

	for _, req := range pkg.GetRequires().Get() {
		depNode, err := r.resolveRecursive(req.Name, req.Constraint, append(path, id))
		if err != nil {
			return nil, err
		}
		node.Deps = append(node.Deps, depNode.ID)
	}

	return node, nil
}

func (r *Resolver) findSource(id string, constraint string) (*buildscript.Source, error) {
	if src, ok := r.sources[id]; ok {
		return src, nil
	}

	repoName, pkgName, _ := api.SplitPackageRef(id)

	buildGo, err := r.repoMgr.FindPackageGo(repoName, pkgName)
	if err == nil {
		src := buildscript.NewSource(id, buildGo, filepath.Dir(buildGo), r.buildscriptOutputDir(id), api.SourceRemote, r.force)
		r.sources[id] = src
		return src, nil
	}

	if !r.repoMgr.IsNative(repoName) {
		return nil, fmt.Errorf("find %s: %w", id, err)
	}

	return r.findNativeSource(id, repoName, pkgName, constraint)
}

func (r *Resolver) findNativeSource(id, repoName, pkgName, constraint string) (*buildscript.Source, error) {
	urlTemplate, err := r.repoMgr.GetNativeURL(repoName)
	if err != nil {
		return nil, err
	}

	gitURL := repo.ResolveNativeURL(urlTemplate, pkgName)
	repoDir := filepath.Join(r.cacheDir, repoName, pkgName, "repo")

	if err := repo.EnsureRepoAtRef(gitURL, repoDir, ""); err != nil {
		return nil, fmt.Errorf("clone %s: %w", gitURL, err)
	}

	tags, err := repo.ListTags(repoDir)
	if err != nil {
		return nil, fmt.Errorf("list tags for %s: %w", id, err)
	}

	versions := repo.FilterValidVersions(tags)
	if len(versions) == 0 {
		return nil, fmt.Errorf("no valid versions found for %s", id)
	}

	selectedVersion, selectedRef, err := repo.SelectNativeVersion(versions, constraint)
	if err != nil {
		return nil, err
	}

	vlog.Info("  %s@%s (ref: %s)", id, selectedVersion, selectedRef)

	if err := repo.EnsureRepoAtRef(gitURL, repoDir, selectedRef); err != nil {
		return nil, fmt.Errorf("checkout %s for %s: %w", selectedRef, id, err)
	}

	buildGo := filepath.Join(repoDir, "build.go")
	if !fs.FileExists(buildGo) {
		return nil, fmt.Errorf("build.go not found in %s", repoDir)
	}

	src := buildscript.NewSource(id, buildGo, repoDir, r.buildscriptOutputDir(id), api.SourceRemote, r.force)

	node := NewPackageNode(id, src, nil, true).WithNative(gitURL, versions, selectedVersion)
	if constraint != "" {
		node.Constraints = append(node.Constraints, constraint)
	}
	r.graph.Packages[id] = node
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

func constraintsCompatible(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	ca, okA := api.ParseConstraint(a)
	cb, okB := api.ParseConstraint(b)
	if !okA || !okB {
		return a == b
	}
	return ca.Match(cb.Version) || cb.Match(ca.Version)
}

func checkNodeConstraints(node *PackageNode, incoming string) error {
	if incoming == "" {
		return nil
	}
	for _, existing := range node.Constraints {
		if !constraintsCompatible(existing, incoming) {
			return fmt.Errorf("conflicting version constraints for %s: '%s' vs '%s'",
				node.ID, existing, incoming)
		}
	}
	node.Constraints = append(node.Constraints, incoming)
	return nil
}

func topologicalSort(packages map[string]*PackageNode) ([]string, error) {
	return toposort.TopologicalSort(packages, func(n *PackageNode) []string { return n.Deps })
}
