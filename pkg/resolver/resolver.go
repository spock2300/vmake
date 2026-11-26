package resolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/pkgvis"
	"github.com/spock2300/vmake/internal/toposort"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
)

type SourceKind string

const (
	SourceKindLocal    SourceKind = "local"
	SourceKindNative   SourceKind = "native"
	SourceKindRegistry SourceKind = "registry"
)

func (k SourceKind) String() string { return string(k) }

// LocalLinkVersion identifies a linked container or source whose version
// selection is fully local and ignores lock/config pins, tags and constraints.
const LocalLinkVersion = "0.0.0-local"

// LocalPathVersion identifies a module container loaded from a relative path
// declared in vmake.json "modules".
const LocalPathVersion = "0.0.0-path"

// ContainerNode represents a resolved module container: the local project
// ("//"), a versioned native Git repository ("<repo>/<module>"), or a registry
// wrapper repository ("<repo>").
type ContainerNode struct {
	ID              string
	Repo            string
	RepoURL         string
	Kind            SourceKind
	Dir             string
	URL             string
	Version         string
	Commit          string
	Versions        map[string]string
	Link             *buildscript.SourceLink
	LocalPath        bool
	DeclaredByRemote bool
	Manifest         *buildscript.Manifest
	Constraints     []string
}

func (c *ContainerNode) IsLocal() bool     { return c != nil && c.Kind == SourceKindLocal }
func (c *ContainerNode) IsNative() bool    { return c != nil && c.Kind == SourceKindNative }
func (c *ContainerNode) IsRegistry() bool  { return c != nil && c.Kind == SourceKindRegistry }
func (c *ContainerNode) IsLinked() bool    { return c != nil && c.Link != nil && c.Link.Path != "" }
func (c *ContainerNode) IsLocalPath() bool { return c != nil && c.LocalPath }

type PackageNode struct {
	ID          string
	Container   string
	RelPath     string
	Kind        SourceKind
	Exported    bool
	Source      *buildscript.Source
	Pkg         *api.Package
	Deps        []string
	Constraints []string
}

type ExportVisibilityError struct {
	From      string
	To        string
	Container string
	Exported  []string
}

func (e *ExportVisibilityError) Error() string {
	if len(e.Exported) > 0 {
		return fmt.Sprintf("package %q is not exported by container %q (requested by %s; exported: %s)", e.To, e.Container, e.From, strings.Join(e.Exported, ", "))
	}
	return fmt.Sprintf("package %q is not exported by container %q (requested by %s)", e.To, e.Container, e.From)
}

func NewPackageNode(id, container, relPath string, kind SourceKind, src *buildscript.Source, pkg *api.Package) *PackageNode {
	return &PackageNode{
		ID:        id,
		Container: container,
		RelPath:   relPath,
		Kind:      kind,
		Source:    src,
		Pkg:       pkg,
		Deps:      []string{},
	}
}

func (n *PackageNode) IsLocal() bool {
	if n == nil {
		return false
	}
	if n.Kind != "" {
		return n.Kind == SourceKindLocal
	}
	return n.Source != nil && n.Source.IsLocal()
}

func (n *PackageNode) IsNative() bool {
	return n != nil && n.Kind == SourceKindNative
}

func (n *PackageNode) IsRegistry() bool {
	return n != nil && n.Kind == SourceKindRegistry
}

func (n *PackageNode) SourceKind() SourceKind {
	if n != nil && n.Kind != "" {
		return n.Kind
	}
	if n.IsLocal() {
		return SourceKindLocal
	}
	return SourceKindRegistry
}

func (n *PackageNode) HasOwnGit() bool {
	return n != nil && n.Pkg != nil && len(n.Pkg.GitURLs()) > 0
}

type Graph struct {
	Containers        map[string]*ContainerNode
	Packages          map[string]*PackageNode
	Roots             []string
	Order             []string
	SourceConstraints map[string][]string
	frozen            bool
}

// CanonicalPackageRef resolves a raw package reference written inside
// callerContainerID to its single canonical PackageNode.ID in this graph,
// honoring container export tables so exported packages never load twice.
func (g *Graph) CanonicalPackageRef(callerContainerID, rawRef string) string {
	if !api.IsLocalPackageID(rawRef) || callerContainerID == "" || callerContainerID == api.LocalContainerID {
		return api.CanonicalizePackageRef(callerContainerID, rawRef)
	}
	if g != nil {
		if c := g.Containers[callerContainerID]; c != nil && c.Manifest != nil && c.Manifest.HasExports() {
			rel, _ := api.LocalRelPath(rawRef)
			return c.Manifest.CanonicalPackageID(callerContainerID, rel)
		}
	}
	return api.CanonicalizePackageRef(callerContainerID, rawRef)
}

// CheckExportVisibility enforces container export whitelists across containers.
// fromPkg may be "@local" for project-level config/CLI operations.
func (g *Graph) CheckExportVisibility(fromPkg, toPkg string) error {
	if g == nil || fromPkg == "" || toPkg == "" || fromPkg == toPkg {
		return nil
	}
	toNode := g.Packages[toPkg]
	if toNode == nil || toNode.Container == "" || toNode.Container == api.LocalContainerID {
		return nil
	}
	toContainer := g.Containers[toNode.Container]
	if toContainer == nil || toContainer.IsLocal() || toContainer.Manifest == nil || !toContainer.Manifest.HasExports() {
		return nil
	}
	if toNode.Exported {
		return nil
	}
	fromContainer := ""
	if fromPkg == "@local" || api.IsLocalPackageID(fromPkg) {
		fromContainer = api.LocalContainerID
	} else if fromNode := g.Packages[fromPkg]; fromNode != nil {
		fromContainer = fromNode.Container
	}
	if fromContainer == toNode.Container {
		return nil
	}
	return &ExportVisibilityError{
		From:      fromPkg,
		To:        toPkg,
		Container: toNode.Container,
		Exported:  toContainer.Manifest.ExportNames(),
	}
}

func NewGraph() *Graph {
	return &Graph{
		Containers:        make(map[string]*ContainerNode),
		Packages:          make(map[string]*PackageNode),
		SourceConstraints: make(map[string][]string),
	}
}

func (g *Graph) Freeze() {
	g.frozen = true
}

func (g *Graph) IsFrozen() bool {
	return g.frozen
}

func (g *Graph) ContainerOf(pkgID string) *ContainerNode {
	if g == nil {
		return nil
	}
	node := g.Packages[pkgID]
	if node == nil {
		return nil
	}
	return g.Containers[node.Container]
}

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
	ctx                context.Context
	sources            map[string]*buildscript.Source
	graph              *Graph
	repoMgr            *repo.RepoManager
	projectRoot        string
	depsDir            string
	sourceMgr          *repo.SourceManager
	dryRunDir          string
	dryRunMgr          *repo.SourceManager
	lockfile           *lockfile.Lock
	lockPolicy         LockPolicy
	configPins         map[string]string
	sourceLinks        map[string]string
	trustChecker       buildscript.ScriptTrustChecker
	repoLocks          map[string]*flock.FileLock
	rootExactOverrides map[string]string
}

func NewResolver(repoMgr *repo.RepoManager, depsDir string) *Resolver {
	return &Resolver{
		ctx:         context.Background(),
		sources:     make(map[string]*buildscript.Source),
		graph:       NewGraph(),
		repoMgr:     repoMgr,
		depsDir:     depsDir,
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
	isLocal := api.IsLocalPackageID(id)
	if r == nil {
		return pkgvis.VisibilityPath(isLocal, id, "", "")
	}
	if r.graph != nil {
		if node, ok := r.graph.Packages[id]; ok && node != nil && node.Source != nil {
			return pkgvis.VisibilityPath(node.IsLocal(), id, r.effectiveProjectRoot(), node.Source.Dir)
		}
	}
	if src, ok := r.sources[id]; ok && src != nil {
		return pkgvis.VisibilityPath(src.IsLocal(), id, r.effectiveProjectRoot(), src.Dir)
	}
	return pkgvis.VisibilityPath(isLocal, id, "", "")
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

// SetSourceLinks maps container IDs or SetGit package IDs to local working
// trees. A linked native container loads its packages from the tree; a linked
// SetGit package replaces only its upstream source tree.
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

type constraintRestart struct {
	containerID string
	constraints []string
}

func (r *Resolver) resetGraph() {
	r.sources = make(map[string]*buildscript.Source)
	r.graph = NewGraph()
}

func resolveEffectiveRoots(projectDir string, manifest *buildscript.Manifest, overrides []string) ([]string, error) {
	if len(overrides) == 0 {
		return manifest.EffectiveRoots(projectDir)
	}
	seen := make(map[string]bool, len(overrides))
	roots := make([]string, 0, len(overrides))
	for _, raw := range overrides {
		root := strings.TrimSpace(raw)
		if err := api.ValidatePackageRef(root); err != nil {
			return nil, fmt.Errorf("config root %q: %w", raw, err)
		}
		if !api.IsLocalPackageID(root) {
			return nil, fmt.Errorf("config root %q must be a local package starting with '//'", raw)
		}
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	return roots, nil
}

func (r *Resolver) ResolveProject(projectDir string, rootOverrides []string) error {
	projectDir = filepath.Clean(projectDir)
	if resolved, err := filepath.EvalSymlinks(projectDir); err == nil {
		projectDir = resolved
	}
	r.projectRoot = projectDir

	manifest, _, err := buildscript.LoadManifest(projectDir)
	if err != nil {
		return err
	}

	roots, err := resolveEffectiveRoots(projectDir, manifest, rootOverrides)
	if err != nil {
		return err
	}

	knownConstraints := make(map[string][]string)
	r.rootExactOverrides = make(map[string]string)
	for mod, spec := range manifest.Modules {
		if spec.Path != "" {
			continue
		}
		if c := normalizeConstraint(spec.Version); c != "" {
			knownConstraints[mod] = appendUnique(knownConstraints[mod], c)
			if isExactConstraint(c) {
				r.rootExactOverrides[mod] = c
			}
		}
	}

	const maxPasses = 8
	for pass := 0; pass < maxPasses; pass++ {
		r.resetGraph()
		localContainer := &ContainerNode{
			ID:       api.LocalContainerID,
			Kind:     SourceKindLocal,
			Dir:      projectDir,
			Manifest: manifest,
		}
		r.graph.Containers[api.LocalContainerID] = localContainer
		r.graph.Roots = slices.Clone(roots)
		r.mergeSourceConstraints(localContainer)

		restart, err := r.resolveRootsPass(localContainer, roots, knownConstraints)
		if err != nil {
			r.Close()
			return err
		}
		if restart != nil {
			knownConstraints[restart.containerID] = restart.constraints
			continue
		}
		if err := r.validateAllSourceNodes(); err != nil {
			r.Close()
			return err
		}
		if err := r.UpdateOrder(); err != nil {
			r.Close()
			return err
		}
		return nil
	}

	r.Close()
	return fmt.Errorf("dependency resolution did not converge after %d constraint intersection passes", maxPasses)
}

func (r *Resolver) resolveRootsPass(localContainer *ContainerNode, roots []string, knownConstraints map[string][]string) (*constraintRestart, error) {
	for _, rootID := range roots {
		relPath, _ := api.LocalRelPath(rootID)
		_, restart, err := r.resolvePackage(rootID, localContainer, relPath, nil, knownConstraints)
		if err != nil || restart != nil {
			return restart, err
		}
	}
	return nil, nil
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
	requireFuncs := api.BindPackageGetRequireFuncs(pkg)
	if len(requireFuncs) == 0 {
		return nil
	}

	api.BindPackageUpdateRequireContext(pkg, cfgVals, options)

	deps := pkg.GetRequires().Get()
	newDeps := make([]string, 0, len(deps))
	for _, req := range deps {
		resolved := r.graph.CanonicalPackageRef(node.Container, req.Name)
		if _, ok := r.graph.Packages[resolved]; !ok {
			return fmt.Errorf("dependency %q (required by %s) not found in dependency graph; declare it unconditionally in OnRequire (use ctx.When() for config-conditional requires)", req.Name, id)
		}
		if err := pkgvis.CheckInternal(id, r.VisibilityPath(id), resolved, r.VisibilityPath(resolved)); err != nil {
			return err
		}
		if err := r.graph.CheckExportVisibility(id, resolved); err != nil {
			return err
		}
		newDeps = append(newDeps, resolved)
	}
	node.Deps = newDeps
	return nil
}

func (r *Resolver) resolvePackage(pkgID string, container *ContainerNode, relPath string, path []string, knownConstraints map[string][]string) (*PackageNode, *constraintRestart, error) {
	if !container.IsLocal() && container.Manifest != nil {
		if expKey, expTarget, collides := container.Manifest.ExportKeyCollision(relPath); collides {
			internalRef := api.LocalContainerID
			if relPath != "" {
				internalRef = api.LocalContainerID + strings.Trim(filepath.ToSlash(relPath), "/")
			}
			return nil, nil, fmt.Errorf("package %q in container %s collides with export %q -> %q; rename the export key or move the internal package", internalRef, container.ID, expKey, expTarget)
		}
	}
	if err := api.CheckCycle(path, pkgID); err != nil {
		return nil, nil, err
	}
	if node, exists := r.graph.Packages[pkgID]; exists {
		if node.Container != container.ID || strings.Trim(filepath.ToSlash(node.RelPath), "/") != strings.Trim(filepath.ToSlash(relPath), "/") {
			return nil, nil, fmt.Errorf("package ID %q collision between %s (%s) and %s (%s)", pkgID, node.Container, node.RelPath, container.ID, relPath)
		}
		return node, nil, nil
	}

	src, err := r.loadPackageSource(container, pkgID, relPath)
	if err != nil {
		return nil, nil, err
	}

	pkg, err := r.PreparePackage(src)
	if err != nil {
		return nil, nil, fmt.Errorf("load %s: %w", pkgID, err)
	}

	if err := r.finalizePackageLink(container, pkgID, src, pkg); err != nil {
		return nil, nil, err
	}
	if err := r.validateLinkedPatches(pkgID, src, pkg); err != nil {
		return nil, nil, err
	}

	r.sources[pkgID] = src
	node := NewPackageNode(pkgID, container.ID, relPath, container.Kind, src, pkg)
	node.Exported = container.IsLocal() || container.Manifest == nil || !container.Manifest.HasExports() || container.Manifest.IsExportedRelPath(relPath)
	r.graph.Packages[pkgID] = node

	if restart, err := r.recurseDeps(node, container, path, knownConstraints); err != nil || restart != nil {
		return nil, restart, err
	}

	return node, nil, nil
}

func (r *Resolver) loadPackageSource(container *ContainerNode, pkgID, relPath string) (*buildscript.Source, error) {
	adapter := adapterForKind(container.Kind)
	if adapter == nil {
		return nil, fmt.Errorf("unsupported container kind %q for %s", container.Kind, container.ID)
	}
	return adapter.loadPackageSource(r, container, pkgID, relPath)
}

func (r *Resolver) finalizePackageLink(container *ContainerNode, pkgID string, src *buildscript.Source, pkg *api.Package) error {
	adapter := adapterForKind(container.Kind)
	if adapter == nil {
		return nil
	}
	return adapter.finalizePackageLink(r, container, pkgID, src, pkg)
}

func (r *Resolver) PreparePackage(src *buildscript.Source) (*api.Package, error) {
	return buildscript.LoadBuildScriptWithTrust(*src, r.trustChecker)
}

func (r *Resolver) validateLinkedPatches(id string, src *buildscript.Source, pkg *api.Package) error {
	if src == nil || src.Link == nil || !src.Link.LocalSource || pkg == nil || len(pkg.GetPatches()) == 0 {
		return nil
	}
	return fmt.Errorf("linked package %s declares patches; patches cannot be applied to a locally linked source tree", id)
}

func (r *Resolver) recurseDeps(node *PackageNode, container *ContainerNode, path []string, knownConstraints map[string][]string) (*constraintRestart, error) {
	if node.Pkg == nil {
		return nil, nil
	}
	nextPath := append(path, node.ID)
	for _, req := range node.Pkg.GetRequires().Get() {
		depContainer, depID, depRel, restart, err := r.resolveDepTarget(node.ID, container, req.Name, knownConstraints)
		if err != nil || restart != nil {
			return restart, err
		}
		depNode, restart, err := r.resolvePackage(depID, depContainer, depRel, nextPath, knownConstraints)
		if err != nil || restart != nil {
			return restart, err
		}
		if err := pkgvis.CheckInternal(node.ID, r.VisibilityPath(node.ID), depNode.ID, r.VisibilityPath(depNode.ID)); err != nil {
			return nil, err
		}
		node.Deps = append(node.Deps, depNode.ID)
	}
	return nil, nil
}

func (r *Resolver) resolveDepTarget(callerID string, callerContainer *ContainerNode, rawRef string, knownConstraints map[string][]string) (*ContainerNode, string, string, *constraintRestart, error) {
	if err := api.ValidatePackageRef(rawRef); err != nil {
		return nil, "", "", nil, fmt.Errorf("package %s: %w", callerID, err)
	}
	if api.IsLocalPackageID(rawRef) {
		relPath, _ := api.LocalRelPath(rawRef)
		depID := r.graph.CanonicalPackageRef(callerContainer.ID, rawRef)
		return callerContainer, depID, relPath, nil, nil
	}
	if callerContainer.Kind == SourceKindRegistry && strings.HasPrefix(rawRef, callerContainer.ID+"/") {
		return callerContainer, rawRef, strings.TrimPrefix(rawRef, callerContainer.ID+"/"), nil, nil
	}
	targetContainerID, relPath, modSpec, ok := callerContainer.Manifest.MatchModule(rawRef)
	if !ok {
		manifestHint := filepath.Join(callerContainer.Dir, buildscript.ManifestFileName)
		return nil, "", "", nil, fmt.Errorf("package %s requires %q, which does not match any module declared in %s; add the container to \"modules\" in vmake.json", callerID, rawRef, manifestHint)
	}
	var (
		depContainer *ContainerNode
		restart      *constraintRestart
		err          error
	)
	if modSpec.Path != "" {
		depContainer, err = r.ensureLocalPathContainer(callerContainer, targetContainerID, modSpec.Path)
	} else {
		depContainer, restart, err = r.ensureContainer(callerContainer, targetContainerID, normalizeConstraint(modSpec.Version), callerID, knownConstraints)
	}
	if err != nil || restart != nil {
		return nil, "", "", restart, err
	}
	if depContainer.Manifest != nil && depContainer.Manifest.HasExports() {
		diskRel, allowed := depContainer.Manifest.ResolveExport(relPath)
		if !allowed {
			return nil, "", "", nil, &ExportVisibilityError{
				From:      callerID,
				To:        rawRef,
				Container: depContainer.ID,
				Exported:  depContainer.Manifest.ExportNames(),
			}
		}
		relPath = diskRel
	}
	return depContainer, rawRef, relPath, nil, nil
}

func resolveContainedDir(rootDir, targetPath string) (string, bool, error) {
	info, err := os.Stat(targetPath)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("%s is not a directory", targetPath)
		}
		return "", false, err
	}
	realRoot := filepath.Clean(rootDir)
	if resolved, err := filepath.EvalSymlinks(rootDir); err == nil {
		realRoot = filepath.Clean(resolved)
	}
	realTarget, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		return "", false, err
	}
	realTarget = filepath.Clean(realTarget)
	rel, err := filepath.Rel(realRoot, realTarget)
	if err != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") || filepath.IsAbs(rel) {
		return realTarget, true, nil
	}
	return realTarget, false, nil
}

func (r *Resolver) ensureLocalPathContainer(callerContainer *ContainerNode, containerID, relDir string) (*ContainerNode, error) {
	repoName, _, _ := api.SplitPackageRef(containerID)
	if linkPath, ok := r.sourceLink(containerID); ok {
		if c, exists := r.graph.Containers[containerID]; exists {
			return c, nil
		}
		c, err := r.resolveLinkedNativeContainer(containerID, repoName, linkPath, "", nil)
		if err != nil {
			return nil, err
		}
		r.graph.Containers[containerID] = c
		r.mergeSourceConstraints(c)
		return c, nil
	}
	rawAbs := filepath.Clean(filepath.Join(callerContainer.Dir, filepath.FromSlash(relDir)))
	absPath, escaped, err := resolveContainedDir(callerContainer.Dir, rawAbs)
	if err != nil {
		return nil, fmt.Errorf("local path container %s directory %s not found", containerID, rawAbs)
	}
	if !callerContainer.IsLocal() && escaped {
		return nil, fmt.Errorf("container %s path %q escapes parent container %s", containerID, relDir, callerContainer.ID)
	}
	if c, exists := r.graph.Containers[containerID]; exists {
		if c.IsLinked() {
			return c, nil
		}
		if !c.IsLocalPath() || filepath.Clean(c.Dir) != filepath.Clean(absPath) {
			return nil, fmt.Errorf("container %s conflicts between local path %s and already-resolved source %s", containerID, absPath, c.Dir)
		}
		return c, nil
	}
	manifest, _, err := buildscript.LoadManifest(absPath)
	if err != nil {
		return nil, err
	}
	commit, err := repo.GetCurrentCommitContext(r.ctx, absPath)
	if err != nil || commit == "" {
		commit = "local-path"
	}
	declaredByRemote := !callerContainer.IsLocal() && (!callerContainer.IsLocalPath() || callerContainer.DeclaredByRemote)
	repoURL := ""
	if declaredByRemote {
		repoName = callerContainer.Repo
		repoURL = callerContainer.RepoURL
	}
	c := &ContainerNode{
		ID:               containerID,
		Repo:             repoName,
		RepoURL:          repoURL,
		Kind:             SourceKindNative,
		Dir:              absPath,
		Version:          LocalPathVersion,
		Commit:           commit,
		LocalPath:        true,
		DeclaredByRemote: declaredByRemote,
		Manifest:         manifest,
	}
	r.graph.Containers[containerID] = c
	r.mergeSourceConstraints(c)
	return c, nil
}

func (r *Resolver) manifestNativeRepoURL(callerContainer *ContainerNode, repoName string) string {
	if callerContainer != nil && callerContainer.Manifest != nil {
		if spec, ok := callerContainer.Manifest.Repos[repoName]; ok && strings.TrimSpace(spec.Native) != "" {
			return strings.TrimSpace(spec.Native)
		}
	}
	if r.graph != nil {
		if root := r.graph.Containers[api.LocalContainerID]; root != nil && root.Manifest != nil {
			if spec, ok := root.Manifest.Repos[repoName]; ok && strings.TrimSpace(spec.Native) != "" {
				return strings.TrimSpace(spec.Native)
			}
		}
	}
	return ""
}

func (r *Resolver) ensureContainer(callerContainer *ContainerNode, containerID, incomingConstraint, requirer string, knownConstraints map[string][]string) (*ContainerNode, *constraintRestart, error) {
	if c, exists := r.graph.Containers[containerID]; exists {
		if c.IsLocalPath() {
			return nil, nil, fmt.Errorf("container %s conflicts between versioned module %q (from %s) and local path container %s", containerID, incomingConstraint, requirer, c.Dir)
		}
		restart, err := r.applyContainerConstraint(c, incomingConstraint, requirer)
		if err != nil || restart != nil {
			return nil, restart, err
		}
		return c, nil, nil
	}

	constraints := slices.Clone(knownConstraints[containerID])
	if incomingConstraint != "" {
		constraints = appendUnique(constraints, incomingConstraint)
	}

	c, err := externalAdapterForID(containerID).resolveContainer(r, callerContainer, containerID, constraints)
	if err != nil {
		return nil, nil, err
	}

	r.graph.Containers[containerID] = c
	r.mergeSourceConstraints(c)
	return c, nil, nil
}

func (r *Resolver) lockRepo(repoName string) error {
	if r.repoMgr == nil || repoName == "" {
		return nil
	}
	if _, ok := r.repoLocks[repoName]; ok {
		return nil
	}
	lock, err := r.repoMgr.Lock(repoName, false)
	if err != nil {
		return err
	}
	r.repoLocks[repoName] = lock
	return nil
}

func (r *Resolver) resolveRegistryContainer(repoName string) (*ContainerNode, error) {
	if err := r.lockRepo(repoName); err != nil {
		return nil, err
	}
	if !r.repoMgr.Exists(repoName) {
		return nil, fmt.Errorf("repository '%s' not found", repoName)
	}
	if r.repoMgr.IsNative(repoName) {
		return nil, fmt.Errorf("native repository %q requires '<repo>/<module>' in vmake.json \"modules\"", repoName)
	}
	repoPath := r.repoMgr.Path(repoName)
	head, headErr := repo.GetCurrentCommitContext(r.ctx, repoPath)
	if headErr != nil {
		head = ""
	}
	if err := r.checkRegistryLock(repoName, head); err != nil {
		return nil, err
	}
	url, _ := r.repoMgr.URL(repoName)
	manifest, _, err := buildscript.LoadManifest(repoPath)
	if err != nil {
		return nil, err
	}
	return &ContainerNode{
		ID:       repoName,
		Repo:     repoName,
		Kind:     SourceKindRegistry,
		Dir:      repoPath,
		URL:      url,
		Commit:   head,
		Manifest: manifest,
	}, nil
}

func (r *Resolver) resolveNativeContainer(containerID, repoName, modName string, constraints []string) (*ContainerNode, error) {
	urlTemplate, err := r.repoMgr.GetNativeURL(repoName)
	if err != nil {
		return nil, err
	}
	return r.resolveNativeContainerWithURL(containerID, repoName, modName, urlTemplate, "", constraints)
}

func (r *Resolver) resolveNativeContainerWithURL(containerID, repoName, modName, urlTemplate, manifestRepoURL string, constraints []string) (*ContainerNode, error) {
	gitURL := repo.ResolveNativeURL(urlTemplate, modName)

	if linkPath, ok := r.sourceLink(containerID); ok {
		c, err := r.resolveLinkedNativeContainer(containerID, repoName, linkPath, gitURL, constraints)
		if err == nil && c != nil {
			c.RepoURL = manifestRepoURL
		}
		return c, err
	}
	if r.sourceMgr == nil {
		return nil, fmt.Errorf("resolver for %s has no source manager configured", containerID)
	}
	if err := r.checkLockedContainer(containerID); err != nil {
		return nil, err
	}

	_, _, hasExplicitPin := r.lockQuery().Containers().PinFor(containerID)
	effectiveConstraints := constraints
	if rootExact := r.rootExactOverrides[containerID]; rootExact != "" && !hasExplicitPin {
		effectiveConstraints = []string{rootExact}
	}

	var (
		commitConstraint  string
		semverConstraints []string
		err               error
	)
	if !hasExplicitPin {
		commitConstraint, semverConstraints, err = SplitConstraints(containerID, effectiveConstraints)
		if err != nil {
			return nil, err
		}
	}

	pinVersion, pinCommit, hasPin := r.pinnedContainerVersion(containerID)

	var (
		versions        map[string]string
		selectedVersion string
		res             *repo.SourceResult
	)
	key := repo.PackageTreeKey(repoName, modName)
	urls := []string{gitURL}

	if hasPin && pinCommit != "" && (r.sourceMgr.HasTreeCommitFromURLs(key, pinCommit, urls) || r.sourceMgr.HasCachedCommit(urls, pinCommit) || r.lockPolicy.Offline) {
		if !hasExplicitPin {
			if pinVersion != "" {
				if err := r.checkConstraints(containerID, pinVersion, semverConstraints); err != nil {
					return nil, err
				}
			}
			if commitConstraint != "" && pinCommit != commitConstraint {
				return nil, fmt.Errorf("locked/pinned commit %s for container %s violates commit constraint %s; run 'vmake deps update' to re-resolve", repo.ShortCommit(pinCommit), containerID, repo.ShortCommit(commitConstraint))
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
			vlog.Verbose("  %s@%s (pinned, cached)", containerID, selectedVersion)
		} else {
			vlog.Verbose("  %s@%s (commit-pinned, cached)", containerID, repo.ShortCommit(pinCommit))
		}
	} else if (hasPin && pinVersion == "" && pinCommit != "") || (!hasPin && commitConstraint != "") {
		targetCommit := pinCommit
		if targetCommit == "" {
			targetCommit = commitConstraint
		}
		res, err = r.ensureNativeSource(key, repo.SourceRequest{
			Key: key, URLs: urls, Commit: targetCommit, ResolveOnly: true,
		})
		if err != nil {
			return nil, err
		}
		vlog.Verbose("  %s@%s", containerID, repo.ShortCommit(res.Commit))
	} else {
		if r.lockPolicy.Offline {
			return nil, fmt.Errorf("offline mode: cannot list tags for %s without network", containerID)
		}
		vlog.Verbose("  %s: listing versions from %s", containerID, strings.Join(urls, ", "))
		tags, err := repo.ListRemoteTagsContext(r.ctx, urls)
		if err != nil {
			return nil, fmt.Errorf("list tags for %s: %w", containerID, err)
		}
		versions = repo.FilterValidVersions(tags)
		if len(versions) == 0 {
			return nil, fmt.Errorf("no valid versions found for %s", containerID)
		}
		selected, ref, err := r.selectNativeContainerVersion(containerID, versions, semverConstraints)
		if err != nil {
			return nil, err
		}
		selectedVersion = selected
		vlog.Verbose("  %s@%s", containerID, selectedVersion)

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

	manifest, _, err := buildscript.LoadManifest(res.SrcDir)
	if err != nil {
		return nil, err
	}
	return &ContainerNode{
		ID:          containerID,
		Repo:        repoName,
		RepoURL:     manifestRepoURL,
		Kind:        SourceKindNative,
		Dir:         res.SrcDir,
		URL:         gitURL,
		Version:     selectedVersion,
		Commit:      res.Commit,
		Versions:    versions,
		Manifest:    manifest,
		Constraints: slices.Clone(constraints),
	}, nil
}

func (r *Resolver) resolveLinkedNativeContainer(containerID, repoName, linkPath, gitURL string, constraints []string) (*ContainerNode, error) {
	commit, err := repo.GetCurrentCommitContext(r.ctx, linkPath)
	if err != nil {
		return nil, fmt.Errorf("read linked native container %s at %s: %w", containerID, linkPath, err)
	}
	manifest, _, err := buildscript.LoadManifest(linkPath)
	if err != nil {
		return nil, err
	}
	return &ContainerNode{
		ID:      containerID,
		Repo:    repoName,
		Kind:    SourceKindNative,
		Dir:     linkPath,
		URL:     gitURL,
		Version: LocalLinkVersion,
		Commit:  commit,
		Link: &buildscript.SourceLink{
			Path:        linkPath,
			Commit:      commit,
			Native:      true,
			LocalSource: true,
		},
		Manifest:    manifest,
		Constraints: slices.Clone(constraints),
	}, nil
}

func (r *Resolver) hasContainerOverride(containerID string) bool {
	if _, _, explicit := r.lockQuery().Containers().PinFor(containerID); explicit {
		return true
	}
	return r.rootExactOverrides[containerID] != ""
}

func isExactConstraint(c string) bool {
	return strings.HasPrefix(c, "=") || repo.IsCommitID(c)
}

func (r *Resolver) applyContainerConstraint(c *ContainerNode, incoming, requirer string) (*constraintRestart, error) {
	if incoming == "" {
		return nil, nil
	}
	if !slices.Contains(c.Constraints, incoming) {
		c.Constraints = append(c.Constraints, incoming)
	}
	if c.Kind != SourceKindNative || c.IsLinked() || c.IsLocalPath() || r.hasContainerOverride(c.ID) {
		return nil, nil
	}
	commitConstraint, semverConstraints, err := SplitConstraints(c.ID, c.Constraints)
	if err != nil {
		return nil, fmt.Errorf("conflicting version constraints for container %s (required by %s): %w", c.ID, requirer, err)
	}
	if commitConstraint != "" {
		if c.Commit == commitConstraint {
			return nil, nil
		}
		return nil, fmt.Errorf("conflicting version constraints for container %s: already selected commit %s (constraints: [%s]), but %s requires %q",
			c.ID, repo.ShortCommit(c.Commit), strings.Join(c.Constraints, ", "), requirer, incoming)
	}
	if c.Version == "" {
		return nil, nil
	}
	if err := checkSingleConstraint(c.ID, c.Version, incoming); err == nil {
		return nil, nil
	}
	if _, _, hasPin := r.pinnedContainerVersion(c.ID); hasPin {
		return nil, fmt.Errorf("version %s for container %s violates constraint %q (required by %s); run 'vmake deps update' to re-resolve",
			c.Version, c.ID, incoming, requirer)
	}
	if len(c.Versions) > 0 {
		if _, _, err := repo.SelectNativeVersionMulti(c.Versions, semverConstraints); err == nil {
			return &constraintRestart{
				containerID: c.ID,
				constraints: slices.Clone(c.Constraints),
			}, nil
		}
	}
	return nil, fmt.Errorf("conflicting version constraints for container %s: already selected %s (constraints: [%s]), but %s requires %q",
		c.ID, c.Version, strings.Join(c.Constraints, ", "), requirer, incoming)
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

func (r *Resolver) lockQuery() LockQuery {
	return LockQuery{Policy: r.lockPolicy, Pins: r.configPins, Lock: r.lockfile}
}

func (r *Resolver) selectNativeContainerVersion(containerID string, versions map[string]string, constraints []string) (string, string, error) {
	decision := r.lockQuery().Containers().Decide(containerID, "", "")
	switch decision.Provenance {
	case ProvenanceTargetCommit, ProvenanceTargetVersion, ProvenanceConfigPin:
		ref, exists := versions[decision.Version]
		if !exists {
			if decision.FromTarget {
				return "", "", fmt.Errorf("requested version %s for %s not found in upstream tags", decision.Version, containerID)
			}
			return "", "", fmt.Errorf("config-pinned version %s for %s not found in upstream tags", decision.Version, containerID)
		}
		return decision.Version, ref, nil
	case ProvenanceLock:
		if decision.Version == "" {
			break
		}
		ref, exists := versions[decision.Version]
		if !exists {
			return "", "", fmt.Errorf("locked version %s for %s not found in upstream tags; run 'vmake deps update %s' to re-resolve", decision.Version, containerID, containerID)
		}
		if err := r.checkConstraints(containerID, decision.Version, constraints); err != nil {
			return "", "", err
		}
		return decision.Version, ref, nil
	}
	selected, ref, err := repo.SelectNativeVersionMulti(versions, constraints)
	if err != nil {
		return "", "", fmt.Errorf("conflicting version constraints for container %s: %w", containerID, err)
	}
	return selected, ref, nil
}

func (r *Resolver) pinnedContainerVersion(id string) (version, commit string, ok bool) {
	decision := r.lockQuery().Containers().Decide(id, "", "")
	switch decision.Provenance {
	case ProvenanceTargetCommit:
		return "", decision.Commit, true
	case ProvenanceTargetVersion:
		return decision.Version, "", true
	case ProvenanceConfigPin, ProvenanceLock:
		return decision.Version, decision.Commit, true
	}
	return "", "", false
}

func (r *Resolver) checkLockedContainer(id string) error {
	if !r.lockPolicy.Locked {
		return nil
	}
	if r.lockfile == nil || r.lockfile.IsEmpty() {
		return fmt.Errorf("--locked: vmake.lock is out of date or missing; run 'vmake deps sync' or 'vmake deps update'")
	}
	query := r.lockQuery().Containers()
	if problem, bad := query.LockEntryProblem(id, true); bad {
		return problem.Error(id)
	}
	if problem, mismatch := query.PinLockMismatch(id); mismatch {
		return problem.Error(id)
	}
	return nil
}

func (r *Resolver) checkRegistryLock(repoName, head string) error {
	if r.lockPolicy.Locked {
		if r.lockfile == nil || r.lockfile.IsEmpty() {
			return fmt.Errorf("--locked: vmake.lock is out of date or missing; run 'vmake deps sync' or 'vmake deps update'")
		}
		query := r.lockQuery().Containers()
		if problem, bad := query.LockEntryProblem(repoName, true); bad {
			return problem.Error(repoName)
		}
		locked, _ := query.rawLockEntry(repoName)
		if head != "" && locked != nil && locked.Commit != head {
			return fmt.Errorf("--locked: registry '%s' content changed since lock (commit %s expected, now %s); run 'vmake deps update' to re-resolve",
				repoName, repo.ShortCommit(locked.Commit), repo.ShortCommit(head))
		}
		return nil
	}
	if !r.lockPolicy.StrictWrapper || head == "" {
		return nil
	}
	locked, ok := r.lockQuery().Containers().LockEntry(repoName)
	if !ok || locked.Commit == "" {
		return nil
	}
	if head != locked.Commit {
		return fmt.Errorf("registry '%s' content changed since lock (commit %s expected, now %s); run 'vmake deps update' to re-resolve",
			repoName, repo.ShortCommit(locked.Commit), repo.ShortCommit(head))
	}
	return nil
}

func (r *Resolver) mergeSourceConstraints(c *ContainerNode) {
	if c == nil || c.Manifest == nil || len(c.Manifest.Sources) == 0 {
		return
	}
	keys := make([]string, 0, len(c.Manifest.Sources))
	for k := range c.Manifest.Sources {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, rawKey := range keys {
		pkgID := r.graph.CanonicalPackageRef(c.ID, rawKey)
		norm := normalizeConstraint(c.Manifest.Sources[rawKey])
		if norm == "" {
			continue
		}
		r.graph.SourceConstraints[pkgID] = appendUnique(r.graph.SourceConstraints[pkgID], norm)
	}
}

func (r *Resolver) validateAllSourceNodes() error {
	ids := make([]string, 0, len(r.graph.Packages))
	for id := range r.graph.Packages {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		node := r.graph.Packages[id]
		if node == nil {
			continue
		}
		if cs := r.graph.SourceConstraints[id]; len(cs) > 0 {
			node.Constraints = slices.Clone(cs)
		}
		if err := r.validateSourceNodeConstraints(node); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) validateSourceNodeConstraints(node *PackageNode) error {
	if node == nil || node.Pkg == nil || !node.HasOwnGit() {
		return nil
	}
	if node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource {
		return nil
	}

	commitConstraint, semverConstraints, err := SplitConstraints(node.ID, node.Constraints)
	if err != nil {
		return fmt.Errorf("conflicting version constraints for source %s: %w", node.ID, err)
	}

	query := r.lockQuery().Sources()
	if target, fromTarget, hasTarget := query.PinFor(node.ID); hasTarget {
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
		} else {
			return fmt.Errorf("package %s declares no versions; specify a 40-character commit SHA instead of %q", node.ID, target)
		}
		return r.checkConstraints(node.ID, target, semverConstraints)
	}

	if locked, lok := query.LockEntry(node.ID); lok {
		if locked.Version != "" {
			if len(node.Pkg.Versions()) > 0 {
				if _, exists := node.Pkg.Versions()[locked.Version]; !exists {
					return fmt.Errorf("locked version %s for %s not found in declared versions; run 'vmake deps update %s' to re-resolve", locked.Version, node.ID, node.ID)
				}
			}
			if err := r.checkConstraints(node.ID, locked.Version, semverConstraints); err != nil {
				return err
			}
		}
		if commitConstraint != "" && locked.Commit != "" && locked.Commit != commitConstraint {
			return fmt.Errorf("locked commit %s for %s violates commit constraint %s; run 'vmake deps update' to re-resolve", repo.ShortCommit(locked.Commit), node.ID, repo.ShortCommit(commitConstraint))
		}
	}

	if len(semverConstraints) > 0 && len(node.Pkg.Versions()) > 0 {
		if _, err := node.Pkg.SelectVersionMulti(semverConstraints); err != nil {
			return fmt.Errorf("conflicting version constraints for %s (constraints: [%s]): %w", node.ID, strings.Join(semverConstraints, ", "), err)
		}
	}
	return nil
}

func (r *Resolver) checkConstraints(id, version string, constraints []string) error {
	for _, c := range constraints {
		if err := checkSingleConstraint(id, version, c); err != nil {
			return err
		}
	}
	return nil
}

func checkSingleConstraint(id, version, constraint string) error {
	constraint = normalizeConstraint(constraint)
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

func SplitConstraints(id string, constraints []string) (commit string, semver []string, err error) {
	for _, raw := range constraints {
		c := normalizeConstraint(raw)
		if c == "" {
			continue
		}
		if repo.IsCommitID(c) {
			if commit != "" && commit != c {
				return "", nil, fmt.Errorf("conflicting commit constraints %s and %s for %s", repo.ShortCommit(commit), repo.ShortCommit(c), id)
			}
			commit = c
			continue
		}
		semver = appendUnique(semver, c)
	}
	if commit != "" && len(semver) > 0 {
		return "", nil, fmt.Errorf("conflicting commit constraint %s and version constraint [%s] for %s", repo.ShortCommit(commit), strings.Join(semver, ", "), id)
	}
	return commit, semver, nil
}

func normalizeConstraint(raw string) string {
	c := strings.TrimSpace(raw)
	if c == "*" {
		return ""
	}
	return c
}

func appendUnique(list []string, item string) []string {
	if item == "" || slices.Contains(list, item) {
		return list
	}
	return append(list, item)
}

func topologicalSort(packages map[string]*PackageNode) ([]string, error) {
	return toposort.TopologicalSort(packages, func(n *PackageNode) []string { return n.Deps })
}
