package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	internalfs "github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

// SourceDisposition names how a build package obtains its source.
type SourceDisposition uint8

const (
	SourceDispositionUnlinked SourceDisposition = iota
	SourceDispositionLinked
	SourceDispositionContainerLinked
	SourceDispositionContainerLinkedOwnGit
)

func (d SourceDisposition) String() string {
	switch d {
	case SourceDispositionUnlinked:
		return "unlinked"
	case SourceDispositionLinked:
		return "source-linked"
	case SourceDispositionContainerLinked:
		return "container-linked"
	case SourceDispositionContainerLinkedOwnGit:
		return "container-linked-own-git"
	default:
		return "unknown"
	}
}

// SourceWorkspace owns a project's materialized source placement and lifecycle.
type SourceWorkspace struct {
	ctx        *RuntimeContext
	projectDir string
	depsDir    string
	links      map[string]string
}

// NewSourceWorkspace derives a workspace from a resolved project model.
func NewSourceWorkspace(ctx *RuntimeContext) *SourceWorkspace {
	w := &SourceWorkspace{ctx: ctx}
	if ctx == nil {
		return w
	}
	if ctx.Paths != nil {
		w.projectDir = ctx.Paths.ProjectDir
		w.depsDir = ctx.Paths.DepsDir
	}
	w.links = ctx.SourceLinks
	return w
}

// NewSourceWorkspaceFromProject derives a workspace without a resolved graph.
func NewSourceWorkspaceFromProject(projectDir, depsDir string) (*SourceWorkspace, error) {
	local, err := config.LoadLocal(projectDir)
	if err != nil {
		return nil, err
	}
	return newSourceWorkspaceAt(projectDir, depsDir, local.Links), nil
}

func newSourceWorkspaceAt(projectDir, depsDir string, links map[string]string) *SourceWorkspace {
	copied := make(map[string]string, len(links))
	for name, path := range links {
		copied[name] = path
	}
	return &SourceWorkspace{
		projectDir: projectDir,
		depsDir:    depsDir,
		links:      copied,
	}
}

// CleanPackage releases one package's outputs and, when all is set, its tree.
func CleanPackage(projectDir, depsDir, name string, all bool) error {
	source, err := newSourceWorkspaceAt(projectDir, depsDir, nil).Package(name)
	if err != nil {
		return err
	}
	if err := source.ReleaseOutputs(); err != nil {
		return err
	}
	if all {
		return source.ReleaseAll()
	}
	return nil
}

func (w *SourceWorkspace) sources() map[string]*PackageSource {
	if w == nil || w.ctx == nil || w.ctx.DepGraph == nil {
		return nil
	}
	sources := make(map[string]*PackageSource, len(w.ctx.DepGraph.Packages))
	for name := range w.ctx.DepGraph.Packages {
		source, err := w.Package(name)
		if err != nil {
			continue
		}
		sources[name] = source
	}
	return sources
}

// Package resolves one package from the resolved model or disk context.
func (w *SourceWorkspace) Package(name string) (*PackageSource, error) {
	if w == nil {
		return nil, fmt.Errorf("source workspace is nil")
	}
	if w.ctx != nil && w.ctx.DepGraph != nil {
		node := w.ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil {
			return nil, fmt.Errorf("package %s has no source", name)
		}
		return w.packageFromNode(name, node), nil
	}
	return w.packageFromDisk(name)
}

func (w *SourceWorkspace) packageFromNode(name string, node *resolver.PackageNode) *PackageSource {
	loc := w.ctx.packageLocation(name, node)
	directLink := node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource && node.Source.Link.Path != node.Source.Dir
	p := &PackageSource{
		name:              name,
		loc:               loc,
		node:              node,
		depsDir:           w.depsDir,
		buildRoot:         node.Source.Dir,
		containerSupplied: loc.ContainerLinked || loc.ContainerLocalPath,
		kind:              node.SourceKind(),
		ownGit:            node.HasOwnGit(),
		disposition:       dispositionFor(loc, node.HasOwnGit(), directLink),
	}
	return p
}

func (ctx *RuntimeContext) packageSource(name string, node *resolver.PackageNode) *PackageSource {
	if ctx == nil || node == nil {
		return nil
	}
	return NewSourceWorkspace(ctx).packageFromNode(name, node)
}

func dispositionFor(loc packageLocation, ownGit, directLink bool) SourceDisposition {
	containerSupplied := loc.ContainerLinked || loc.ContainerLocalPath
	switch {
	case directLink:
		return SourceDispositionLinked
	case containerSupplied && ownGit:
		return SourceDispositionContainerLinkedOwnGit
	case containerSupplied:
		return SourceDispositionContainerLinked
	case loc.Linked:
		return SourceDispositionLinked
	default:
		return SourceDispositionUnlinked
	}
}

func (w *SourceWorkspace) packageFromDisk(name string) (*PackageSource, error) {
	if w.projectDir == "" {
		return nil, fmt.Errorf("source workspace for %s has no project directory", name)
	}
	if err := api.ValidatePackageRef(name); err != nil {
		return nil, fmt.Errorf("invalid package reference %q: %w", name, err)
	}
	if w.depsDir == "" {
		return nil, fmt.Errorf("source workspace for %s has no dependency directory", name)
	}
	var source *PackageSource
	if rel, ok := api.LocalRelPath(name); ok {
		source = w.localFromDisk(name, rel)
	} else {
		repoName, rest, ok := api.SplitPackageRef(name)
		if !ok || repoName == "" || rest == "" {
			return nil, fmt.Errorf("invalid package reference %q", name)
		}
		if mod, member, hasMember := strings.Cut(rest, "/"); hasMember {
			source = w.nativeMemberFromDisk(name, repoName+"/"+mod, member)
		} else {
			source = w.remoteOwnerFromDisk(name, repoName, rest)
		}
	}
	if err := w.validateSource(source); err != nil {
		return nil, err
	}
	return source, nil
}

func (w *SourceWorkspace) validateSource(source *PackageSource) error {
	if source == nil {
		return fmt.Errorf("empty source placement")
	}
	if member := filepath.FromSlash(source.loc.Member); member != "" && !filepath.IsLocal(member) {
		return fmt.Errorf("package %s member path escapes its container: %s", source.name, source.loc.Member)
	}
	for _, root := range []string{source.loc.ThirdPartyRoot, source.loc.TreeRoot, source.OutputDir()} {
		if root == "" {
			continue
		}
		if err := internalfs.ValidateManagedPath(w.depsDir, root); err != nil {
			return fmt.Errorf("package %s: %w", source.name, err)
		}
	}
	return nil
}

func (w *SourceWorkspace) localFromDisk(name, rel string) *PackageSource {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	packageRoot := w.projectDir
	if rel != "" {
		packageRoot = filepath.Join(packageRoot, filepath.FromSlash(rel))
	}
	loc := packageLocation{
		Name:        name,
		Container:   api.LocalContainerID,
		Member:      rel,
		PackageRoot: packageRoot,
		SourceTree:  filepath.Join(packageRoot, "src"),
		TreeRoot:    filepath.Join(w.depsDir, "local", build.MemberDir(rel)),
	}
	directLink := w.links[name] != ""
	disposition := SourceDispositionUnlinked
	if directLink {
		loc.Linked = true
		loc.SourceTree = w.resolveLinkPath(w.links[name])
		disposition = SourceDispositionLinked
	}
	return &PackageSource{
		name:              name,
		loc:               loc,
		depsDir:           w.depsDir,
		buildRoot:         packageRoot,
		containerSupplied: false,
		kind:              resolver.SourceKindLocal,
		ownGit:            true,
		disposition:       disposition,
	}
}

func (w *SourceWorkspace) nativeMemberFromDisk(name, containerID, member string) *PackageSource {
	ownerRoot := filepath.Join(w.depsDir, filepath.FromSlash(containerID))
	containerRoot, internal, containerLinked, localPath := w.resolveContainerRoot(containerID, ownerRoot, member)
	memberDir := build.MemberDir(internal)
	loc := packageLocation{
		Name:               name,
		Container:          containerID,
		Owner:              containerID,
		Member:             internal,
		OwnerTreeRoot:      ownerRoot,
		PackageRoot:        joinMember(containerRoot, internal),
		SourceTree:         filepath.Join(ownerRoot, "third_party", memberDir, "src"),
		ThirdPartyRoot:     filepath.Join(ownerRoot, "third_party", memberDir),
		TreeRoot:           filepath.Join(ownerRoot, "third_party", memberDir),
		ContainerLinked:    containerLinked,
		ContainerLocalPath: localPath,
	}
	directLink := w.links[name] != ""
	disposition := SourceDispositionUnlinked
	if directLink {
		loc.Linked = true
		loc.SourceTree = w.resolveLinkPath(w.links[name])
		loc.TreeRoot = ownerRoot
		disposition = SourceDispositionLinked
	} else if containerLinked || localPath {
		disposition = SourceDispositionContainerLinkedOwnGit
	}
	return &PackageSource{
		name:        name,
		loc:         loc,
		depsDir:     w.depsDir,
		buildRoot:   loc.PackageRoot,
		kind:        resolver.SourceKindNative,
		ownGit:      true,
		disposition: disposition,
	}
}

func (w *SourceWorkspace) remoteOwnerFromDisk(name, repoName, owner string) *PackageSource {
	ownerRoot := filepath.Join(w.depsDir, repoName, owner)
	loc := packageLocation{
		Name:          name,
		Container:     name,
		Owner:         name,
		OwnerTreeRoot: ownerRoot,
		PackageRoot:   filepath.Join(ownerRoot, "src"),
		SourceTree:    filepath.Join(ownerRoot, "src"),
		TreeRoot:      ownerRoot,
	}
	disposition := SourceDispositionUnlinked
	if path := w.links[name]; path != "" {
		loc.PackageRoot = w.resolveLinkPath(path)
		loc.SourceTree = loc.PackageRoot
		if _, err := os.Stat(filepath.Join(loc.PackageRoot, "vmake.json")); err == nil {
			loc.ContainerLinked = true
			disposition = SourceDispositionContainerLinked
		} else {
			loc.Linked = true
			disposition = SourceDispositionLinked
		}
	}
	return &PackageSource{
		name:        name,
		loc:         loc,
		depsDir:     w.depsDir,
		buildRoot:   loc.PackageRoot,
		kind:        resolver.SourceKind(""),
		ownGit:      false,
		disposition: disposition,
	}
}

func (w *SourceWorkspace) resolveLinkPath(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(w.projectDir, path)
}

func joinMember(root, member string) string {
	if member == "" {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(member))
}

func (w *SourceWorkspace) resolveContainerRoot(containerID, ownerRoot, member string) (root, internal string, linked, localPath bool) {
	type candidate struct {
		dir  string
		link bool
		path bool
	}
	var candidates []candidate
	if path := w.links[containerID]; path != "" {
		candidates = append(candidates, candidate{dir: w.resolveLinkPath(path), link: true})
	}
	if w.projectDir != "" {
		if manifest, _, err := buildscript.LoadManifest(w.projectDir); err == nil && manifest != nil {
			if spec, ok := manifest.Modules[containerID]; ok && strings.TrimSpace(spec.Path) != "" {
				dir := filepath.FromSlash(strings.TrimSpace(spec.Path))
				if !filepath.IsAbs(dir) {
					dir = filepath.Join(w.projectDir, dir)
				}
				candidates = append(candidates, candidate{dir: dir, path: true})
			}
		}
	}
	candidates = append(candidates, candidate{dir: filepath.Join(ownerRoot, "src")})
	internal = strings.Trim(filepath.ToSlash(member), "/")
	for _, c := range candidates {
		manifest, found, err := buildscript.LoadManifest(c.dir)
		if err != nil || !found || manifest == nil || !manifest.HasExports() {
			continue
		}
		if rel, ok := manifest.ResolveExport(internal); ok && rel != "" {
			return c.dir, rel, c.link, c.path
		}
		return c.dir, internal, c.link, c.path
	}
	for _, c := range candidates {
		if c.link || c.path {
			return c.dir, internal, c.link, c.path
		}
	}
	return filepath.Join(ownerRoot, "src"), internal, false, false
}

// PackageSource is the per-package handle into a SourceWorkspace.
type PackageSource struct {
	name              string
	loc               packageLocation
	node              *resolver.PackageNode
	depsDir           string
	buildRoot         string
	containerSupplied bool
	disposition       SourceDisposition
	kind              resolver.SourceKind
	ownGit            bool
}

func (p *PackageSource) Member() string                 { return p.loc.Member }
func (p *PackageSource) PackageRoot() string            { return p.loc.PackageRoot }
func (p *PackageSource) BuildRoot() string              { return p.buildRoot }
func (p *PackageSource) SourceTree() string             { return p.loc.SourceTree }
func (p *PackageSource) OwnerTreeRoot() string          { return p.loc.OwnerTreeRoot }
func (p *PackageSource) ThirdPartyRoot() string         { return p.loc.ThirdPartyRoot }
func (p *PackageSource) TreeRoot() string               { return p.loc.TreeRoot }
func (p *PackageSource) Disposition() SourceDisposition { return p.disposition }
func (p *PackageSource) Linked() bool                   { return p.loc.Linked }
func (p *PackageSource) ContainerLinked() bool          { return p.loc.ContainerLinked }
func (p *PackageSource) ContainerLocalPath() bool       { return p.loc.ContainerLocalPath }

// SourceOverride is the source root used when the package builds in a linked tree.
func (p *PackageSource) SourceOverride() string {
	if p.containerSupplied || (p.kind == resolver.SourceKindRegistry && p.disposition == SourceDispositionLinked) {
		return p.loc.PackageRoot
	}
	return ""
}

// OutputDir is the directory holding every build key for this package.
func (p *PackageSource) OutputDir() string {
	root := p.outputRoot()
	if root == "" {
		return ""
	}
	if p.loc.Member == "" {
		return root
	}
	return memberOutputDir(p.loc.OwnerTreeRoot, p.loc.Member)
}

func (p *PackageSource) outputRoot() string {
	if p.kind == resolver.SourceKindLocal || p.loc.OwnerTreeRoot == "" {
		return ""
	}
	return filepath.Join(p.loc.OwnerTreeRoot, "out")
}

func memberOutputDir(base, member string) string {
	return filepath.Join(base, "out", build.MemberDir(filepath.ToSlash(member)))
}

// ApplyRequest fills the placement-derived Key and Root of a source request.
func (p *PackageSource) ApplyRequest(req *repo.SourceRequest) {
	if p == nil || req == nil {
		return
	}
	req.Key = p.ensureKey()
	if p.ownGit && p.loc.ThirdPartyRoot != "" {
		req.Root = p.loc.ThirdPartyRoot
		return
	}
	req.Root = ""
}

func (p *PackageSource) ensureKey() string {
	if p.kind == resolver.SourceKindLocal {
		rel := p.loc.Member
		if rel == "" {
			rel, _ = api.LocalRelPath(p.name)
		}
		return repo.PackageTreeKey("", build.MemberDir(rel))
	}
	return p.name
}

// Ensure materializes this package through the supplied manager.
func (p *PackageSource) Ensure(req repo.SourceRequest, mgr *repo.SourceManager) (*repo.SourceResult, error) {
	if mgr == nil {
		return nil, fmt.Errorf("source manager is nil")
	}
	p.ApplyRequest(&req)
	return mgr.EnsureSource(req)
}

// Bind applies the placement to a package after its layout is known.
func (p *PackageSource) Bind(pkg *api.Package, dirs api.PkgDirs, seed string, dryRun bool) error {
	if p == nil || pkg == nil {
		return nil
	}
	if p.node == nil {
		return fmt.Errorf("bind %s: source workspace has no resolved node", p.name)
	}
	oldSourceDir := pkg.SourceDir()
	pkg.SetDirs(dirs)
	if !p.ownGit {
		if p.kind == resolver.SourceKindLocal {
			return nil
		}
		return p.rebindSrcDir(pkg, oldSourceDir, dirs)
	}
	if p.disposition == SourceDispositionLinked {
		pkg.SetSrcDir(p.loc.SourceTree)
		return nil
	}
	if p.kind == resolver.SourceKindLocal {
		if seed == "" {
			return fmt.Errorf("package %s has no materialized source tree", p.name)
		}
		if dryRun {
			pkg.SetSrcDir(seed)
			return nil
		}
		link := filepath.Join(p.loc.PackageRoot, "src")
		if err := internalfs.EnsureSymlink(link, seed); err != nil {
			return err
		}
		pkg.SetSrcDir(link)
		return nil
	}
	if err := p.rebindSrcDir(pkg, oldSourceDir, dirs); err != nil {
		return err
	}
	if p.kind == resolver.SourceKindRegistry {
		return nil
	}
	if dryRun && seed != "" {
		pkg.SetSrcDir(seed)
		return nil
	}
	pkg.SetSrcDir(p.loc.SourceTree)
	return nil
}

func (p *PackageSource) rebindSrcDir(pkg *api.Package, oldSourceDir string, dirs api.PkgDirs) error {
	if p.node == nil || p.node.Source == nil {
		return nil
	}
	raw := pkg.SrcDirRaw()
	if raw == "" {
		return nil
	}
	oldRoot := oldSourceDir
	if oldRoot == "" {
		oldRoot = p.node.Source.Dir
	}
	rebased, err := rebaseSourcePath(raw, oldRoot, dirs.SourceDir, p.loc.Member, p.node.Source.Dir)
	if err != nil {
		return err
	}
	pkg.SetSrcDir(rebased)
	return nil
}

// ReleaseSource removes source and state while keeping outputs and sibling third_party trees.
func (p *PackageSource) ReleaseSource(mgr *repo.SourceManager) error {
	if p == nil || mgr == nil {
		return fmt.Errorf("source manager is nil")
	}
	if err := p.removeLocalLink(); err != nil {
		return err
	}
	root := p.loc.ThirdPartyRoot
	if root == "" {
		root = p.loc.TreeRoot
	}
	if root == "" {
		return nil
	}
	return mgr.CleanMaterializedSourceAt(root)
}

func (p *PackageSource) removeLocalLink() error {
	if p.kind != resolver.SourceKindLocal {
		return nil
	}
	link := filepath.Join(p.loc.PackageRoot, "src")
	if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(link)
	}
	return nil
}

// ReleaseOutputs removes only this package's build outputs.
func (p *PackageSource) ReleaseOutputs() error {
	dir := p.OutputDir()
	if dir == "" {
		return nil
	}
	if err := internalfs.ValidateManagedPath(p.depsDir, dir); err != nil {
		return err
	}
	return internalfs.RemoveAll(dir)
}

// ReleaseAll removes both the source tree and this package's outputs.
func (p *PackageSource) ReleaseAll() error {
	if p == nil {
		return fmt.Errorf("source package is nil")
	}
	if err := p.removeLocalLink(); err != nil {
		return err
	}
	if p.loc.TreeRoot != "" {
		if err := internalfs.ValidateManagedPath(p.depsDir, p.loc.TreeRoot); err != nil {
			return err
		}
		if err := internalfs.RemoveAll(p.loc.TreeRoot); err != nil {
			return err
		}
	}
	if dir := p.OutputDir(); dir != "" && (p.loc.TreeRoot == "" || !pathWithin(p.loc.TreeRoot, dir)) {
		if err := internalfs.ValidateManagedPath(p.depsDir, dir); err != nil {
			return err
		}
		if err := internalfs.RemoveAll(dir); err != nil {
			return err
		}
	}
	return nil
}

// MaterializedTree is one raw tree root under the dependency directory.
type MaterializedTree struct {
	Root string
}

// Trees lists every materialized tree root under the dependency directory.
func (w *SourceWorkspace) Trees() []MaterializedTree {
	if w == nil || w.depsDir == "" {
		return nil
	}
	var trees []MaterializedTree
	_ = filepath.WalkDir(w.depsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "state.json" {
			return nil
		}
		trees = append(trees, MaterializedTree{Root: filepath.Dir(path)})
		return nil
	})
	sort.Slice(trees, func(i, j int) bool { return trees[i].Root < trees[j].Root })
	return trees
}

// ReleaseOutputsAll removes all materialized remote outputs.
func (w *SourceWorkspace) ReleaseOutputsAll() {
	if w == nil || w.depsDir == "" {
		return
	}
	for _, tree := range w.Trees() {
		CleanOutputDirs(filepath.Join(tree.Root, "out"), filepath.Base(tree.Root))
	}
	for name := range w.links {
		if api.IsLocalPackageID(name) {
			continue
		}
		owner := name
		if repoName, rest, ok := api.SplitPackageRef(name); ok {
			if mod, _, hasMember := strings.Cut(rest, "/"); hasMember {
				owner = repoName + "/" + mod
			}
		}
		CleanOutputDirs(filepath.Join(w.depsDir, filepath.FromSlash(owner), "out"), owner)
	}
}
