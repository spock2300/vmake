package resolver

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/toposort"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
)

type NativePackageInfo struct {
	GitURL   string
	Versions map[string]string
	Selected string
	Commit   string
}

type PackageNode struct {
	ID          string
	Source      *buildscript.Source
	Pkg         *api.Package
	Deps        []string
	Native      *NativePackageInfo
	Constraints []string
}

func NewPackageNode(id string, src *buildscript.Source, pkg *api.Package) *PackageNode {
	return &PackageNode{
		ID:     id,
		Source: src,
		Pkg:    pkg,
		Deps:   []string{},
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
	frozen   bool
}

func (g *Graph) Freeze() {
	g.frozen = true
}

func (g *Graph) IsFrozen() bool {
	return g.frozen
}

type Resolver struct {
	sources      map[string]*buildscript.Source
	graph        *Graph
	repoMgr      *repo.RepoManager
	depsDir      string
	sourceMgr    *repo.SourceManager
	subParents   map[string]string
	lockfile     *lockfile.Lock
	ignoreLock   bool
	configPins   map[string]string
	trustChecker buildscript.ScriptTrustChecker
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

func (r *Resolver) SetSourceManager(sm *repo.SourceManager) {
	r.sourceMgr = sm
}

func (r *Resolver) SetLockfile(l *lockfile.Lock, ignore bool) {
	r.lockfile = l
	r.ignoreLock = ignore
}

func (r *Resolver) SetTrustChecker(c buildscript.ScriptTrustChecker) {
	r.trustChecker = c
}

func (r *Resolver) SetConfigPins(pins map[string]string) {
	r.configPins = pins
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
	if r.graph.frozen {
		return fmt.Errorf("UpdateOrder: graph is frozen (post-filter mutations forbidden)")
	}
	order, err := topologicalSort(r.graph.Packages)
	if err != nil {
		return fmt.Errorf("dependency cycle detected: %w", err)
	}
	r.graph.Order = order
	return nil
}

func (r *Resolver) ResolveAll(localSources []buildscript.Source) error {
	for _, src := range localSources {
		s := buildscript.NewSource(src.Name, src.Path, src.Dir, api.SourceLocal)
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

func (r *Resolver) FilterDeps(id string, cfgVals map[string]any, options map[string]*api.Option) error {
	if r.graph.frozen {
		return fmt.Errorf("FilterDeps: graph is frozen (post-filter mutations forbidden)")
	}
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
		resolved := r.resolveDepName(id, req.Name)
		if _, ok := r.graph.Packages[resolved]; !ok {
			return fmt.Errorf("dependency %q (required by %s) not found in dependency graph; declare it unconditionally in OnRequire (use ctx.When() for config-conditional requires)", req.Name, id)
		}
		newDeps = append(newDeps, resolved)
	}
	node.Deps = newDeps
	return nil
}

func (r *Resolver) resolveRecursive(id string, constraint string, path []string) (*PackageNode, error) {
	if err := api.CheckCycle(path, id); err != nil {
		return nil, err
	}

	if node, exists := r.graph.Packages[id]; exists {
		if err := validateNodeConstraints(node, constraint, path); err != nil {
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
		if err := validateNodeConstraints(node, constraint, path); err != nil {
			return nil, err
		}
		return node, nil
	}

	node, err := r.resolveOne(id, src, path)
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

func (r *Resolver) resolveOne(id string, src *buildscript.Source, path []string) (*PackageNode, error) {
	pkg, err := r.PreparePackage(src)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", id, err)
	}

	return r.resolveFromCache(id, pkg, src, path)
}

func (r *Resolver) PreparePackage(src *buildscript.Source) (*api.Package, error) {
	return buildscript.LoadBuildScriptWithTrust(*src, r.trustChecker)
}

func (r *Resolver) resolveFromCache(id string, pkg *api.Package, src *buildscript.Source, path []string) (*PackageNode, error) {
	node := NewPackageNode(id, src, pkg)
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
		if werr := r.checkWrapperCommit(id, repoName); werr != nil {
			return nil, werr
		}
		src := buildscript.NewSource(id, buildGo, filepath.Dir(buildGo), api.SourceRemote)
		src.Repo = repoName
		r.sources[id] = src
		return src, nil
	}

	if !r.repoMgr.IsNative(repoName) {
		return nil, fmt.Errorf("find %s: %w", id, err)
	}

	return r.findNativeSource(id, repoName, pkgName, constraint)
}

func (r *Resolver) findNativeSource(id, repoName, pkgName, constraint string) (*buildscript.Source, error) {
	if r.sourceMgr == nil {
		return nil, fmt.Errorf("resolver for %s has no source manager configured", id)
	}
	urlTemplate, err := r.repoMgr.GetNativeURL(repoName)
	if err != nil {
		return nil, err
	}

	gitURL := repo.ResolveNativeURL(urlTemplate, pkgName)

	pkgStub := api.NewPackage()
	pkgStub.Repo = repoName
	pkgStub.Name = pkgName
	pkgStub.SetGit(gitURL)

	pinVersion, pinCommit, hasPin := r.pinnedVersion(id)

	var versions map[string]string
	var selectedVersion string
	var res *repo.SourceResult

	if hasPin && r.sourceMgr.HasMaterializedVersion(pkgStub, pinVersion) {
		if err := r.checkConstraint(id, pinVersion, constraint); err != nil {
			return nil, err
		}
		res, err = r.sourceMgr.EnsureVersion(pkgStub, pinVersion, pinCommit)
		if err != nil {
			return nil, err
		}
		versions = map[string]string{pinVersion: repo.DescribeTag(filepath.Join(res.VersionDir, "src"))}
		selectedVersion = pinVersion
		vlog.Info("  %s@%s (pinned, cached)", id, selectedVersion)
	} else {
		refsDir, err := r.sourceMgr.EnsureRefsClone(pkgStub, !hasPin)
		if err != nil {
			return nil, fmt.Errorf("refs clone for %s: %w", id, err)
		}

		tags, err := repo.ListTags(refsDir)
		if err != nil {
			return nil, fmt.Errorf("list tags for %s: %w", id, err)
		}
		versions = repo.FilterValidVersions(tags)
		if hasPin && versions[pinVersion] == "" {
			refsDir, err = r.sourceMgr.EnsureRefsClone(pkgStub, true)
			if err != nil {
				return nil, fmt.Errorf("refs clone for %s: %w", id, err)
			}
			tags, err = repo.ListTags(refsDir)
			if err != nil {
				return nil, fmt.Errorf("list tags for %s: %w", id, err)
			}
			versions = repo.FilterValidVersions(tags)
		}
		if len(versions) == 0 {
			return nil, fmt.Errorf("no valid versions found for %s", id)
		}
		pkgStub.SetVersions(versions)

		selectedVersion, _, err = r.selectNativeVersion(id, versions, constraint)
		if err != nil {
			return nil, err
		}

		vlog.Info("  %s@%s", id, selectedVersion)

		expectedCommit := ""
		if selectedVersion == pinVersion {
			expectedCommit = pinCommit
		}
		res, err = r.sourceMgr.EnsureVersion(pkgStub, selectedVersion, expectedCommit)
		if err != nil {
			return nil, err
		}
	}

	buildGo := filepath.Join(res.LocalSrc, "build.go")
	if !fs.FileExists(buildGo) {
		return nil, fmt.Errorf("build.go not found in %s", res.LocalSrc)
	}
	src := buildscript.NewSource(id, buildGo, res.LocalSrc, api.SourceRemote)
	src.Repo = repoName

	r.sources[id] = src

	pkg, err := r.PreparePackage(src)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", id, err)
	}

	node := NewPackageNode(id, src, pkg).WithNative(gitURL, versions, selectedVersion)
	node.Native.Commit = res.Commit
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

	r.scanSubPackages(id, res.LocalSrc)

	return src, nil
}

func (r *Resolver) selectNativeVersion(id string, versions map[string]string, constraint string) (string, string, error) {
	if pin, ok := r.configPins[id]; ok && pin != "" {
		ref, exists := versions[pin]
		if !exists {
			return "", "", fmt.Errorf("config-pinned version %s for %s not found in upstream tags", pin, id)
		}
		if err := r.checkConstraint(id, pin, constraint); err != nil {
			return "", "", err
		}
		return pin, ref, nil
	}
	if locked, ok := r.lockfileEntry(id); ok {
		ref, exists := versions[locked.Version]
		if !exists {
			return "", "", fmt.Errorf("locked version %s for %s not found in upstream tags; run 'vmake lock update' to re-resolve", locked.Version, id)
		}
		if err := r.checkConstraint(id, locked.Version, constraint); err != nil {
			return "", "", err
		}
		return locked.Version, ref, nil
	}
	return repo.SelectNativeVersion(versions, constraint)
}

func (r *Resolver) pinnedVersion(id string) (version, commit string, ok bool) {
	if v, ok := r.configPins[id]; ok && v != "" {
		return v, "", true
	}
	if locked, ok := r.lockfileEntry(id); ok {
		return locked.Version, locked.Commit, true
	}
	return "", "", false
}

func (r *Resolver) checkConstraint(id, version, constraint string) error {
	if constraint == "" {
		return nil
	}
	c, ok := api.ParseConstraint(constraint)
	if !ok {
		return fmt.Errorf("invalid constraint %q for %s", constraint, id)
	}
	v, ok := api.ParseVersion(version)
	if !ok || !c.Match(v) {
		return fmt.Errorf("version %s for %s violates constraint %q; run 'vmake lock update' to re-resolve", version, id, constraint)
	}
	return nil
}

func (r *Resolver) lockfileEntry(id string) (*lockfile.LockedPkg, bool) {
	if r.lockfile == nil || r.ignoreLock {
		return nil, false
	}
	locked, ok := r.lockfile.Get(id)
	if !ok || locked.Version == "" {
		return nil, false
	}
	return locked, true
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
	repoName, _, _ := api.SplitPackageRef(parentID)
	for i := range subs {
		ss := &subs[i]
		ss.Repo = repoName
		r.sources[ss.Name] = ss
		r.subParents[ss.Name] = parentID
	}
	vlog.Info("  %s: found %d sub-package(s)", parentID, len(subs))
}

func (r *Resolver) checkoutNativeSource(id, gitURL, repoDir, ref string) (*buildscript.Source, error) {
	if err := repo.EnsureRepoAtRef(gitURL, repoDir, ref); err != nil {
		return nil, fmt.Errorf("checkout %s for %s: %w", ref, id, err)
	}

	buildGo := filepath.Join(repoDir, "build.go")
	if !fs.FileExists(buildGo) {
		return nil, fmt.Errorf("build.go not found in %s", repoDir)
	}

	return buildscript.NewSource(id, buildGo, repoDir, api.SourceRemote), nil
}

func (r *Resolver) checkWrapperCommit(id, repoName string) error {
	locked, ok := r.lockfileEntry(id)
	if !ok || locked.WrapperCommit == "" {
		return nil
	}
	head, err := repo.GetCurrentCommit(r.repoMgr.Path(repoName))
	if err != nil {
		return fmt.Errorf("read HEAD of registry '%s': %w", repoName, err)
	}
	if head != locked.WrapperCommit {
		return fmt.Errorf("registry '%s' content changed since lock (wrapper commit %s expected, now %s); run 'vmake lock update' to re-resolve",
			repoName, repo.ShortCommit(locked.WrapperCommit), repo.ShortCommit(head))
	}
	return nil
}

func requirerName(path []string) string {
	if len(path) == 0 {
		return "?"
	}
	return path[len(path)-1]
}

func validateNodeConstraints(node *PackageNode, constraint string, path []string) error {
	if constraint == "" {
		return nil
	}
	if !slices.Contains(node.Constraints, constraint) {
		node.Constraints = append(node.Constraints, constraint)
	}
	requirer := requirerName(path)

	if node.Native != nil && node.Native.Selected != "" {
		v, ok := api.ParseVersion(node.Native.Selected)
		if !ok {
			return nil
		}
		c, ok := api.ParseConstraint(constraint)
		if !ok {
			return fmt.Errorf("invalid constraint %q for %s (required by %s)", constraint, node.ID, requirer)
		}
		if !c.Match(v) {
			return fmt.Errorf("conflicting version constraints for %s: already selected %s (constraints: [%s]), but %s requires %q",
				node.ID, node.Native.Selected, strings.Join(node.Constraints, ", "), requirer, constraint)
		}
		return nil
	}

	if node.Pkg != nil && len(node.Pkg.Versions()) > 0 {
		if _, err := node.Pkg.SelectVersionMulti(node.Constraints); err != nil {
			return fmt.Errorf("conflicting version constraints for %s (constraints: [%s], required by %s): %w",
				node.ID, strings.Join(node.Constraints, ", "), requirer, err)
		}
	}
	return nil
}

func topologicalSort(packages map[string]*PackageNode) ([]string, error) {
	return toposort.TopologicalSort(packages, func(n *PackageNode) []string { return n.Deps })
}
