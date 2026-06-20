package resolver

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/toposort"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
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
	sources          map[string]*buildscript.Source
	graph            *Graph
	repoMgr          *repo.RepoManager
	depsDir          string
	globalSourcesDir string
	force            bool
	subParents       map[string]string
}

func NewResolver(repoMgr *repo.RepoManager, depsDir string) *Resolver {
	return &Resolver{
		sources:    make(map[string]*buildscript.Source),
		graph:      &Graph{Packages: make(map[string]*PackageNode)},
		repoMgr:    repoMgr,
		depsDir:    depsDir,
		subParents: make(map[string]string),
	}
}

func (r *Resolver) SetForce(force bool) {
	r.force = force
}

func (r *Resolver) SetGlobalSourcesDir(dir string) {
	r.globalSourcesDir = dir
}

func (r *Resolver) SubParents() map[string]string {
	return r.subParents
}

func (r *Resolver) resolveDepName(fromPkg, depName string) string {
	return api.ResolveSubPackageName(fromPkg, depName, r.subParents, func(candidate string) bool {
		_, exists := r.sources[candidate]
		return exists
	})
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

// ResolveDeferred is retained as a no-op for backward compatibility.
// All packages (registry and native) are now resolved eagerly during
// ResolveAll, so there are no deferred nodes to resolve. The method
// still runs UpdateOrder to ensure graph.Order is current.
func (r *Resolver) ResolveDeferred() error {
	return r.UpdateOrder()
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
		newDeps = append(newDeps, r.resolveDepName(id, req.Name))
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

	// Post-lookup: findNativeSource may have registered a deferred node for native packages
	// (native repos must be cloned before build.go can be read for OnRequire).
	if node, exists := r.graph.Packages[id]; exists {
		if err := checkNodeConstraints(node, constraint); err != nil {
			return nil, err
		}
		return node, nil
	}

	// Compile + load + recurse into OnRequire dependencies
	node, err := r.resolvePackage(id, src, path)
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

func (r *Resolver) resolvePackage(id string, src *buildscript.Source, path []string) (*PackageNode, error) {
	scriptPath := r.scriptPath(src)
	if !r.force && src.Path != "" && r.hasCachedScript(scriptPath, src.Path) {
		pkg, err := r.loadPackageFromCache(scriptPath, *src)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", id, err)
		}
		return r.resolveFromCache(id, pkg, src, path)
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

	if err := r.recurseDeps(node, path); err != nil {
		return nil, err
	}
	return node, nil
}

func (r *Resolver) recurseDeps(node *PackageNode, path []string) error {
	pkg := node.Pkg
	if pkg == nil {
		return nil
	}
	for _, req := range pkg.GetRequires().Get() {
		depName := r.resolveDepName(node.ID, req.Name)
		depNode, err := r.resolveRecursive(depName, req.Constraint, append(path, node.ID))
		if err != nil {
			return err
		}
		node.Deps = append(node.Deps, depNode.ID)
	}
	return nil
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
	globalDir := filepath.Join(r.globalSourcesDir, repoName, pkgName)
	localSrc := filepath.Join(r.depsDir, repoName, pkgName, "src")

	selectedVersion, selectedRef, versions, err := r.resolveNativeVersion(id, gitURL, globalDir, localSrc, constraint)
	if err != nil {
		return nil, err
	}

	vlog.Info("  %s@%s (ref: %s)", id, selectedVersion, selectedRef)

	src, err := r.checkoutNativeSource(id, gitURL, localSrc, selectedRef)
	if err != nil {
		return nil, err
	}

	r.sources[id] = src

	pkg, err := r.PreparePackage(src)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", id, err)
	}

	node := NewPackageNode(id, src, pkg, false).WithNative(gitURL, versions, selectedVersion)
	if constraint != "" {
		node.Constraints = append(node.Constraints, constraint)
	}
	r.graph.Packages[id] = node

	if err := r.recurseDeps(node, []string{id}); err != nil {
		return nil, err
	}

	if len(node.Constraints) > 0 && len(pkg.Versions()) > 0 {
		if _, err := pkg.SelectVersionMulti(node.Constraints); err != nil {
			return nil, fmt.Errorf("package %s: %w", id, err)
		}
	}

	r.scanSubPackages(id, localSrc)

	return src, nil
}

func (r *Resolver) scanSubPackages(parentID, checkoutDir string) {
	subs, err := buildscript.ScanSubPackages(checkoutDir, parentID)
	if err != nil {
		vlog.Error("scan sub-packages for %s: %v", parentID, err)
		return
	}
	if len(subs) == 0 {
		return
	}
	for i := range subs {
		ss := &subs[i]
		ss.OutputDir = r.buildscriptOutputDir(ss.Name)
		r.sources[ss.Name] = ss
		r.subParents[ss.Name] = parentID
	}
	vlog.Info("  %s: found %d sub-package(s)", parentID, len(subs))
}

func (r *Resolver) resolveNativeVersion(id, gitURL, globalDir, localSrc, constraint string) (string, string, map[string]string, error) {
	lock, err := flock.Acquire(globalDir)
	if err != nil {
		return "", "", nil, fmt.Errorf("acquire lock for %s: %w", id, err)
	}
	defer lock.Release()

	globalSrc := filepath.Join(globalDir, "src")
	if err := fs.EnsureDir(globalSrc); err != nil {
		return "", "", nil, fmt.Errorf("create global src dir for %s: %w", id, err)
	}

	if err := repo.EnsureRepoAtRef(gitURL, globalSrc, ""); err != nil {
		return "", "", nil, fmt.Errorf("clone %s: %w", gitURL, err)
	}

	if err := fs.EnsureSymlink(localSrc, globalSrc); err != nil {
		return "", "", nil, fmt.Errorf("create symlink for %s: %w", id, err)
	}

	tags, err := repo.ListTags(localSrc)
	if err != nil {
		return "", "", nil, fmt.Errorf("list tags for %s: %w", id, err)
	}

	versions := repo.FilterValidVersions(tags)
	if len(versions) == 0 {
		return "", "", nil, fmt.Errorf("no valid versions found for %s", id)
	}

	selectedVersion, selectedRef, err := repo.SelectNativeVersion(versions, constraint)
	if err != nil {
		return "", "", nil, err
	}

	return selectedVersion, selectedRef, versions, nil
}

func (r *Resolver) checkoutNativeSource(id, gitURL, repoDir, ref string) (*buildscript.Source, error) {
	if err := repo.EnsureRepoAtRef(gitURL, repoDir, ref); err != nil {
		return nil, fmt.Errorf("checkout %s for %s: %w", ref, id, err)
	}

	buildGo := filepath.Join(repoDir, "build.go")
	if !fs.FileExists(buildGo) {
		return nil, fmt.Errorf("build.go not found in %s", repoDir)
	}

	return buildscript.NewSource(id, buildGo, repoDir, r.buildscriptOutputDir(id), api.SourceRemote, r.force), nil
}

func (r *Resolver) scriptPath(src *buildscript.Source) string {
	return filepath.Join(src.GetOutputDir(), "build.so")
}

func (r *Resolver) buildscriptOutputDir(name string) string {
	return filepath.Join(r.depsDir, name)
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
	return nil
}

func topologicalSort(packages map[string]*PackageNode) ([]string, error) {
	return toposort.TopologicalSort(packages, func(n *PackageNode) []string { return n.Deps })
}
