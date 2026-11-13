package resolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/pkgvis"
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

// LocalLinkVersion identifies a package whose source and version selection are
// fully local. Linked packages ignore lock/config pins, tags and constraints.
const LocalLinkVersion = "0.0.0-local"

type LockPolicy struct {
	IgnoreAll     bool
	Float         map[string]bool
	Targets       map[string]string
	StrictWrapper bool
	Locked        bool
	Offline       bool
	DryRun        bool
}

type Resolver struct {
	ctx          context.Context
	sources      map[string]*buildscript.Source
	graph        *Graph
	repoMgr      *repo.RepoManager
	projectRoot  string
	depsDir      string
	sourceMgr    *repo.SourceManager
	dryRunDir    string
	dryRunMgr    *repo.SourceManager
	subParents   map[string]string
	lockfile     *lockfile.Lock
	lockPolicy   LockPolicy
	configPins   map[string]string
	sourceLinks  map[string]string
	trustChecker buildscript.ScriptTrustChecker
	repoLocks    map[string]*flock.FileLock
}

func NewResolver(repoMgr *repo.RepoManager, depsDir string) *Resolver {
	return &Resolver{
		ctx:         context.Background(),
		sources:     make(map[string]*buildscript.Source),
		graph:       &Graph{Packages: make(map[string]*PackageNode)},
		repoMgr:     repoMgr,
		depsDir:     depsDir,
		subParents:  make(map[string]string),
		sourceLinks: make(map[string]string),
		repoLocks:   make(map[string]*flock.FileLock),
	}
}

func (r *Resolver) SetContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.ctx = ctx
}

func (r *Resolver) SetSourceManager(sm *repo.SourceManager) {
	r.sourceMgr = sm
}

func (r *Resolver) SetProjectRoot(dir string) {
	r.projectRoot = dir
}

func (r *Resolver) effectiveProjectRoot() string {
	if r == nil {
		return ""
	}
	if r.projectRoot != "" {
		return r.projectRoot
	}
	if r.depsDir != "" && filepath.Base(filepath.Clean(r.depsDir)) == ".vmake_deps" {
		return filepath.Dir(filepath.Clean(r.depsDir))
	}
	return ""
}

func (r *Resolver) VisibilityPath(id string) string {
	if r == nil {
		return pkgvis.VisibilityPath(false, id, "", "")
	}
	if r.graph != nil {
		if node, ok := r.graph.Packages[id]; ok && node != nil && node.Source != nil {
			return pkgvis.VisibilityPath(node.IsLocal(), id, r.effectiveProjectRoot(), node.Source.Dir)
		}
	}
	if src, ok := r.sources[id]; ok && src != nil {
		return pkgvis.VisibilityPath(src.IsLocal(), id, r.effectiveProjectRoot(), src.Dir)
	}
	return pkgvis.VisibilityPath(false, id, "", "")
}

func (r *Resolver) SetLockfile(l *lockfile.Lock, ignore bool) {
	r.lockfile = l
	r.lockPolicy.IgnoreAll = ignore
}

func (r *Resolver) SetLockPolicy(policy LockPolicy) {
	r.lockPolicy = policy
}

func (r *Resolver) SetTrustChecker(c buildscript.ScriptTrustChecker) {
	r.trustChecker = c
}

// Close releases the shared repository locks held since resolution. Callers
// that finish an invocation must call it; until then a repository cannot be
// updated by another process, and holding the locks for the process lifetime
// would block an in-process exclusive lock (flock is not reentrant).
func (r *Resolver) Close() {
	if r == nil {
		return
	}
	for _, lock := range r.repoLocks {
		_ = lock.Release()
	}
	r.repoLocks = make(map[string]*flock.FileLock)
	if r.dryRunDir != "" {
		_ = os.RemoveAll(r.dryRunDir)
		r.dryRunDir = ""
		r.dryRunMgr = nil
	}
}

func (r *Resolver) SetConfigPins(pins map[string]string) {
	r.configPins = pins
}

// SetSourceLinks maps package IDs to local working trees. A linked native
// package loads build.go and sub-packages from the tree; a linked registry
// package keeps its registry wrapper build.go and replaces only the source
// tree.
func (r *Resolver) SetSourceLinks(links map[string]string) {
	r.sourceLinks = links
	if r.sourceLinks == nil {
		r.sourceLinks = make(map[string]string)
	}
}

func (r *Resolver) sourceLink(id string) (string, bool) {
	path, ok := r.sourceLinks[id]
	return path, ok && path != ""
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

func subPackageHint(fromPkg, depName, resolved string, subParents map[string]string) string {
	if resolved != depName {
		return ""
	}
	candidates := api.SubPackageCandidates(fromPkg, depName, subParents)
	if len(candidates) == 0 {
		return ""
	}
	return fmt.Sprintf(" (tried sub-package candidates: %s)", strings.Join(candidates, ", "))
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
		s := src
		s.Origin = api.SourceLocal
		r.sources[s.Name] = &s
	}

	for _, src := range localSources {
		if _, exists := r.graph.Packages[src.Name]; exists {
			continue
		}
		if _, err := r.resolveRecursive(src.Name, "", nil); err != nil {
			r.Close()
			return err
		}
	}

	if err := r.UpdateOrder(); err != nil {
		r.Close()
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
			hint := subPackageHint(id, req.Name, resolved, r.subParents)
			return fmt.Errorf("dependency %q (required by %s) not found in dependency graph; declare it unconditionally in OnRequire (use ctx.When() for config-conditional requires)%s", req.Name, id, hint)
		}
		if err := pkgvis.CheckInternal(id, r.VisibilityPath(id), resolved, r.VisibilityPath(resolved)); err != nil {
			return err
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
		if err := r.validatePinnedNodeConstraints(node); err != nil {
			return nil, err
		}
		return node, nil
	}

	src, err := r.findSource(id, constraint, path)
	if err != nil {
		return nil, err
	}

	// Post-lookup: findNativeSource may have registered a deferred node for native packages
	// (native repos must be cloned before build.go can be read for OnRequire).
	if node, exists := r.graph.Packages[id]; exists {
		if err := validateNodeConstraints(node, constraint, path); err != nil {
			return nil, err
		}
		if err := r.validatePinnedNodeConstraints(node); err != nil {
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

	if err := r.validatePinnedNodeConstraints(node); err != nil {
		return nil, err
	}
	if len(node.Constraints) > 0 && node.Pkg != nil && len(node.Pkg.Versions()) > 0 &&
		!(node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource) {
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
	if src.Link != nil && src.Link.Native {
		if _, isSub := r.subParents[id]; isSub && len(pkg.GitURLs()) > 0 {
			src.Link.LocalSource = false
		}
	}
	if err := r.validateLinkedPatches(id, src, pkg); err != nil {
		return nil, err
	}

	return r.resolveFromCache(id, pkg, src, path)
}

func (r *Resolver) PreparePackage(src *buildscript.Source) (*api.Package, error) {
	return buildscript.LoadBuildScriptWithTrust(*src, r.trustChecker)
}

func (r *Resolver) validateLinkedPatches(id string, src *buildscript.Source, pkg *api.Package) error {
	if src == nil || src.Link == nil || pkg == nil || len(pkg.GetPatches()) == 0 {
		return nil
	}
	if src.Link.Native {
		if _, isSub := r.subParents[id]; isSub && len(pkg.GitURLs()) > 0 {
			return nil
		}
	}
	return fmt.Errorf("linked package %s declares patches; patches are only supported for native sub-packages with their own git source", id)
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
		if err := pkgvis.CheckInternal(node.ID, r.VisibilityPath(node.ID), depNode.ID, r.VisibilityPath(depNode.ID)); err != nil {
			return err
		}
		node.Deps = append(node.Deps, depNode.ID)
	}
	return nil
}

func (r *Resolver) findSource(id string, constraint string, path []string) (*buildscript.Source, error) {
	if src, ok := r.sources[id]; ok && src.IsLocal() {
		return src, nil
	}
	if r.repoMgr != nil {
		repoName, _, _ := api.SplitPackageRef(id)
		if repoName != "" {
			if _, ok := r.repoLocks[repoName]; !ok {
				lock, err := r.repoMgr.Lock(repoName, false)
				if err != nil {
					return nil, err
				}
				r.repoLocks[repoName] = lock
			}
		}
	}
	if src, ok := r.sources[id]; ok {
		return src, nil
	}

	repoName, pkgName, _ := api.SplitPackageRef(id)

	buildGo, err := r.repoMgr.FindPackageGo(repoName, pkgName)
	if err == nil {
		linkPath, linked := r.sourceLink(id)
		if !linked {
			if werr := r.checkWrapperCommit(id, repoName); werr != nil {
				return nil, werr
			}
		}
		src := buildscript.NewSource(id, buildGo, filepath.Dir(buildGo), api.SourceRemote)
		src.Repo = repoName
		scriptOpts, optsErr := buildscript.DetectScriptOptions(filepath.Dir(buildGo),
			r.repoMgr.Path(repoName))
		if optsErr != nil {
			return nil, optsErr
		}
		src.Script = scriptOpts
		// Trust binds to the repository identity. Record the wrapper commit for
		// build identity and reporting only.
		if head, headErr := repo.GetCurrentCommitContext(r.ctx, r.repoMgr.Path(repoName)); headErr == nil {
			src.Commit = head
		}
		if linked {
			linkCommit, headErr := repo.GetCurrentCommitContext(r.ctx, linkPath)
			if headErr != nil {
				return nil, fmt.Errorf("read linked source %s: %w", linkPath, headErr)
			}
			src.Link = &buildscript.SourceLink{
				Path:        linkPath,
				Commit:      linkCommit,
				LocalSource: true,
			}
		}
		r.sources[id] = src
		return src, nil
	}

	if !r.repoMgr.IsNative(repoName) {
		return nil, fmt.Errorf("find %s: %w", id, err)
	}

	src, nerr := r.findNativeSource(id, repoName, pkgName, constraint, path)
	if nerr != nil && strings.Contains(pkgName, "/") {
		if owner := r.resolvedSubPackageOwner(id); owner != "" {
			return nil, fmt.Errorf("sub-package %q not found under resolved parent package %q: %w", id, owner, nerr)
		}
		return nil, fmt.Errorf("%w (if %q is a native sub-package, its parent must be resolved first: list the parent before the sub-package in AddRequires)", nerr, id)
	}
	return src, nerr
}

func (r *Resolver) resolvedSubPackageOwner(id string) string {
	parts := strings.Split(id, "/")
	for i := len(parts) - 1; i >= 2; i-- {
		parent := strings.Join(parts[:i], "/")
		if _, ok := r.sources[parent]; ok {
			return parent
		}
		if _, ok := r.graph.Packages[parent]; ok {
			return parent
		}
	}
	return ""
}

func (r *Resolver) findNativeLink(id, repoName, constraint, linkPath, gitURL string, path []string) (*buildscript.Source, error) {
	buildGo := filepath.Join(linkPath, "build.go")
	if !fs.FileExists(buildGo) {
		return nil, fmt.Errorf("linked native package %s has no build.go in %s", id, linkPath)
	}
	commit, err := repo.GetCurrentCommitContext(r.ctx, linkPath)
	if err != nil {
		return nil, fmt.Errorf("read linked native package %s at %s: %w", id, linkPath, err)
	}
	src := buildscript.NewSource(id, buildGo, linkPath, api.SourceRemote)
	src.Repo = repoName
	src.URL = gitURL
	src.Commit = commit
	src.Link = &buildscript.SourceLink{
		Path:        linkPath,
		Commit:      commit,
		Native:      true,
		LocalSource: true,
	}
	scriptOpts, optsErr := buildscript.DetectScriptOptions(linkPath, linkPath)
	if optsErr != nil {
		return nil, optsErr
	}
	src.Script = scriptOpts
	r.sources[id] = src

	pkg, err := r.PreparePackage(src)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", id, err)
	}
	if err := r.validateLinkedPatches(id, src, pkg); err != nil {
		return nil, err
	}

	node := NewPackageNode(id, src, pkg).WithNative(gitURL, nil, LocalLinkVersion)
	node.Native.Commit = commit
	if constraint != "" {
		node.Constraints = append(node.Constraints, constraint)
	}
	r.graph.Packages[id] = node

	if err := r.recurseDeps(node, path); err != nil {
		return nil, err
	}
	r.scanSubPackages(id, linkPath)
	return src, nil
}

func (r *Resolver) findNativeSource(id, repoName, pkgName, constraint string, path []string) (*buildscript.Source, error) {
	if r.sourceMgr == nil {
		return nil, fmt.Errorf("resolver for %s has no source manager configured", id)
	}
	urlTemplate, err := r.repoMgr.GetNativeURL(repoName)
	if err != nil {
		return nil, err
	}

	gitURL := repo.ResolveNativeURL(urlTemplate, pkgName)

	if linkPath, ok := r.sourceLink(id); ok {
		return r.findNativeLink(id, repoName, constraint, linkPath, gitURL, path)
	}
	if err := r.checkLockedEntry(id); err != nil {
		return nil, err
	}

	pkgStub := api.NewPackage()
	pkgStub.Repo = repoName
	pkgStub.Name = pkgName
	pkgStub.SetGit(gitURL)

	pinVersion, pinCommit, hasPin := r.pinnedVersion(id)

	var versions map[string]string
	var selectedVersion string
	var res *repo.SourceResult
	key := repo.PackageTreeKey(repoName, pkgName)
	urls := []string{gitURL}

	if hasPin && pinCommit != "" && (r.sourceMgr.HasTreeCommitFromURLs(key, pinCommit, urls) || r.sourceMgr.HasCachedCommit(urls, pinCommit) || r.lockPolicy.Offline) {
		if pinVersion != "" {
			if err := r.checkConstraint(id, pinVersion, constraint); err != nil {
				return nil, err
			}
		}
		res, err = r.ensureNativeSource(key, repo.SourceRequest{Key: key, URLs: urls, Version: pinVersion, Commit: pinCommit, ResolveOnly: true})
		if err != nil {
			return nil, err
		}
		if pinVersion != "" {
			tag, err := repo.DescribeTagContext(r.ctx, res.SrcDir)
			if err != nil {
				return nil, err
			}
			if tag == "" {
				tag = pinVersion
			}
			versions = map[string]string{pinVersion: tag}
			selectedVersion = pinVersion
			vlog.Verbose("  %s@%s (pinned, cached)", id, selectedVersion)
		} else {
			selectedVersion = ""
			vlog.Verbose("  %s@%s (commit-pinned, cached)", id, repo.ShortCommit(pinCommit))
		}
	} else if hasPin && pinVersion == "" && pinCommit != "" {
		res, err = r.ensureNativeSource(key, repo.SourceRequest{
			Key: key, URLs: urls, Commit: pinCommit, ResolveOnly: true,
		})
		if err != nil {
			return nil, err
		}
		selectedVersion = ""
		vlog.Verbose("  %s@%s", id, repo.ShortCommit(res.Commit))
	} else {
		if r.lockPolicy.Offline {
			return nil, fmt.Errorf("offline mode: cannot list tags for %s without network", id)
		}
		vlog.Verbose("  %s: listing versions from %s", id, strings.Join(urls, ", "))
		tags, err := repo.ListRemoteTagsContext(r.ctx, urls)
		if err != nil {
			return nil, fmt.Errorf("list tags for %s: %w", id, err)
		}
		versions = repo.FilterValidVersions(tags)
		if len(versions) == 0 {
			return nil, fmt.Errorf("no valid versions found for %s", id)
		}
		pkgStub.SetVersions(versions)

		selected, ref, err := r.selectNativeVersion(id, versions, constraint)
		if err != nil {
			return nil, err
		}
		selectedVersion = selected

		vlog.Verbose("  %s@%s", id, selectedVersion)

		expectedCommit := ""
		if selectedVersion == pinVersion {
			expectedCommit = pinCommit
		}
		res, err = r.ensureNativeSource(key, repo.SourceRequest{
			Key: key, URLs: urls, Version: selectedVersion, Ref: ref, Commit: expectedCommit, ResolveOnly: true,
		})
		if err != nil {
			return nil, err
		}
	}

	buildGo := filepath.Join(res.SrcDir, "build.go")
	if !fs.FileExists(buildGo) {
		return nil, fmt.Errorf("build.go not found in %s", res.SrcDir)
	}
	src := buildscript.NewSource(id, buildGo, res.SrcDir, api.SourceRemote)
	src.Repo = repoName
	src.URL = gitURL
	src.Commit = res.Commit
	scriptOpts, optsErr := buildscript.DetectScriptOptions(res.SrcDir, res.SrcDir)
	if optsErr != nil {
		return nil, optsErr
	}
	src.Script = scriptOpts

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

	if err := r.recurseDeps(node, path); err != nil {
		return nil, err
	}

	if len(node.Constraints) > 0 && len(pkg.Versions()) > 0 &&
		!(src.Link != nil && src.Link.LocalSource) {
		if _, err := pkg.SelectVersionMulti(node.Constraints); err != nil {
			return nil, fmt.Errorf("package %s: %w", id, err)
		}
	}

	r.scanSubPackages(id, res.SrcDir)

	return src, nil
}

func (r *Resolver) ensureNativeSource(key string, req repo.SourceRequest) (*repo.SourceResult, error) {
	if r.lockPolicy.Offline {
		req.Offline = true
	}
	if !r.lockPolicy.DryRun {
		return r.sourceMgr.EnsureSource(req)
	}
	if req.Commit != "" && r.sourceMgr.HasTreeCommitFromURLs(key, req.Commit, req.URLs) {
		root := r.sourceMgr.TreeDir(key)
		url := ""
		if len(req.URLs) > 0 {
			url = req.URLs[0]
		}
		return &repo.SourceResult{Root: root, SrcDir: filepath.Join(root, "src"), URL: url, Commit: req.Commit}, nil
	}
	if r.dryRunMgr == nil {
		dir, err := os.MkdirTemp("", "vmake-dryrun-*")
		if err != nil {
			return nil, fmt.Errorf("create dry-run temp dir: %w", err)
		}
		r.dryRunDir = dir
		r.dryRunMgr = repo.NewSourceManager(dir, r.sourceMgr.CacheDir()).WithSession(r.sourceMgr.Session()).WithContext(r.ctx)
	}
	return r.dryRunMgr.EnsureSource(req)
}

func (r *Resolver) targetOrPin(id string) (target string, fromTarget bool, ok bool) {
	if r.lockPolicy.Targets != nil {
		if t := strings.TrimSpace(r.lockPolicy.Targets[id]); t != "" {
			return t, true, true
		}
	}
	if r.lockPolicy.Float != nil && r.lockPolicy.Float[id] {
		return "", false, false
	}
	if r.configPins != nil {
		if p := strings.TrimSpace(r.configPins[id]); p != "" {
			return p, false, true
		}
	}
	return "", false, false
}

func (r *Resolver) selectNativeVersion(id string, versions map[string]string, constraint string) (string, string, error) {
	if target, fromTarget, ok := r.targetOrPin(id); ok {
		ref, exists := versions[target]
		if !exists {
			if fromTarget {
				return "", "", fmt.Errorf("requested version %s for %s not found in upstream tags", target, id)
			}
			return "", "", fmt.Errorf("config-pinned version %s for %s not found in upstream tags", target, id)
		}
		if err := r.checkConstraint(id, target, constraint); err != nil {
			return "", "", err
		}
		return target, ref, nil
	}
	if locked, ok := r.lockfileEntry(id); ok && locked.Version != "" {
		ref, exists := versions[locked.Version]
		if !exists {
			return "", "", fmt.Errorf("locked version %s for %s not found in upstream tags; run 'vmake deps update %s' to re-resolve", locked.Version, id, id)
		}
		if err := r.checkConstraint(id, locked.Version, constraint); err != nil {
			return "", "", err
		}
		return locked.Version, ref, nil
	}
	return repo.SelectNativeVersion(versions, constraint)
}

func (r *Resolver) pinnedVersion(id string) (version, commit string, ok bool) {
	if target, fromTarget, hasTarget := r.targetOrPin(id); hasTarget {
		if repo.IsCommitID(target) {
			return "", target, true
		}
		if !fromTarget && r.lockfile != nil {
			if locked, lok := r.lockfile.Get(id); lok && locked.Version == target && locked.Commit != "" {
				return target, locked.Commit, true
			}
		}
		return target, "", true
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
		return fmt.Errorf("version %s for %s violates constraint %q; run 'vmake deps update' to re-resolve", version, id, constraint)
	}
	return nil
}

func (r *Resolver) lockfileEntry(id string) (*lockfile.LockedPkg, bool) {
	if r.lockfile == nil || r.lockPolicy.IgnoreAll {
		return nil, false
	}
	if r.lockPolicy.Float != nil && r.lockPolicy.Float[id] {
		return nil, false
	}
	if r.lockPolicy.Targets != nil && strings.TrimSpace(r.lockPolicy.Targets[id]) != "" {
		return nil, false
	}
	locked, ok := r.lockfile.Get(id)
	if !ok || (locked.Version == "" && locked.Commit == "") {
		return nil, false
	}
	return locked, true
}

func (r *Resolver) scanSubPackages(parentID, checkoutDir string) {
	if resolved, err := filepath.EvalSymlinks(checkoutDir); err != nil {
		vlog.Error("scan sub-packages for %s: resolve %s: %v", parentID, checkoutDir, err)
	} else {
		checkoutDir = resolved
	}
	subs, err := buildscript.ScanSubPackages(checkoutDir, parentID)
	if err != nil {
		vlog.Error("scan sub-packages for %s: %v", parentID, err)
		return
	}
	if len(subs) == 0 {
		return
	}
	repoName, _, _ := api.SplitPackageRef(parentID)
	var parentCommit, parentURL string
	var parentLink *buildscript.SourceLink
	if node := r.graph.Packages[parentID]; node != nil {
		if node.Native != nil {
			parentCommit = node.Native.Commit
			parentURL = node.Native.GitURL
		}
		if node.Source != nil && node.Source.Link != nil && node.Source.Link.Native {
			parentLink = node.Source.Link
		}
	}
	for i := range subs {
		ss := &subs[i]
		ss.Repo = repoName
		ss.Commit = parentCommit
		ss.URL = parentURL
		if parentLink != nil {
			rel, relErr := filepath.Rel(checkoutDir, ss.Dir)
			if relErr == nil {
				linkDir := filepath.Join(parentLink.Path, rel)
				ss.Dir = linkDir
				ss.Path = filepath.Join(linkDir, "build.go")
				ss.Link = &buildscript.SourceLink{
					Path:        linkDir,
					Commit:      parentLink.Commit,
					Native:      true,
					LocalSource: true,
				}
				ss.Commit = parentLink.Commit
			}
		}
		r.sources[ss.Name] = ss
		r.subParents[ss.Name] = parentID
	}
	vlog.Verbose("  %s: found %d sub-package(s)", parentID, len(subs))
}

func (r *Resolver) checkLockedEntry(id string) error {
	if !r.lockPolicy.Locked {
		return nil
	}
	if r.lockfile == nil || len(r.lockfile.Packages) == 0 {
		return fmt.Errorf("--locked: vmake.lock is out of date or missing; run 'vmake deps sync' or 'vmake deps update'")
	}
	locked, ok := r.lockfile.Get(id)
	if !ok || locked.Commit == "" {
		return fmt.Errorf("--locked: package %s is not locked in vmake.lock; run 'vmake deps sync' or 'vmake deps update'", id)
	}
	if r.configPins != nil {
		if pin := strings.TrimSpace(r.configPins[id]); pin != "" {
			if (repo.IsCommitID(pin) && locked.Commit != pin) || (!repo.IsCommitID(pin) && locked.Version != pin) {
				return fmt.Errorf("--locked: package %s is pinned to %s in .vmake/project.json, but vmake.lock has %s; run 'vmake deps sync'", id, pin, lockfile.DisplayVersion(locked.Version, locked.Commit))
			}
		}
	}
	return nil
}

func (r *Resolver) checkWrapperCommit(id, repoName string) error {
	if !r.lockPolicy.StrictWrapper {
		return nil
	}
	locked, ok := r.lockfileEntry(id)
	if !ok || locked.WrapperCommit == "" {
		return nil
	}
	head, err := repo.GetCurrentCommitContext(r.ctx, r.repoMgr.Path(repoName))
	if err != nil {
		return fmt.Errorf("read HEAD of registry '%s': %w", repoName, err)
	}
	if head != locked.WrapperCommit {
		return fmt.Errorf("registry '%s' content changed since lock (wrapper commit %s expected, now %s); run 'vmake deps update' to re-resolve",
			repoName, repo.ShortCommit(locked.WrapperCommit), repo.ShortCommit(head))
	}
	return nil
}

func (r *Resolver) validatePinnedNodeConstraints(node *PackageNode) error {
	if node == nil || node.Native != nil || node.Pkg == nil {
		return nil
	}
	if node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource {
		return nil
	}
	if target, fromTarget, hasTarget := r.targetOrPin(node.ID); hasTarget {
		if repo.IsCommitID(target) {
			return nil
		}
		if len(node.Pkg.Versions()) > 0 {
			if _, exists := node.Pkg.Versions()[target]; !exists {
				if fromTarget {
					return fmt.Errorf("requested version %s for %s not found in declared versions", target, node.ID)
				}
				return fmt.Errorf("config-pinned version %s for %s not found in declared versions", target, node.ID)
			}
		} else if len(node.Pkg.GitURLs()) > 0 {
			return fmt.Errorf("package %s declares no versions; specify a 40-character commit SHA instead of %q", node.ID, target)
		}
		for _, c := range node.Constraints {
			if err := r.checkConstraint(node.ID, target, c); err != nil {
				return err
			}
		}
		return nil
	}
	if locked, lok := r.lockfileEntry(node.ID); lok && locked.Version != "" {
		if len(node.Pkg.Versions()) > 0 {
			if _, exists := node.Pkg.Versions()[locked.Version]; !exists {
				return fmt.Errorf("locked version %s for %s not found in declared versions; run 'vmake deps update %s' to re-resolve", locked.Version, node.ID, node.ID)
			}
		}
		for _, c := range node.Constraints {
			if err := r.checkConstraint(node.ID, locked.Version, c); err != nil {
				return err
			}
		}
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
	if node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource {
		return nil
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
