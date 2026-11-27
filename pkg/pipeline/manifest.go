package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	exec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitcmd"
	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/internal/quiet"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/version"
)

type InstallManifestEntry struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Source   string `json:"source"`
	URL      string `json:"url,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Path     string `json:"path,omitempty"`
	Linked   bool   `json:"linked,omitempty"`
	LinkPath string `json:"linkPath,omitempty"`
}

func (e InstallManifestEntry) isPathModule() bool {
	return e.Source == resolver.SourceKindNative.String() && e.Version == resolver.LocalPathVersion && e.URL == "" && e.Path != ""
}

func (e InstallManifestEntry) pathModuleDir(root string) (string, os.FileInfo, error) {
	if e.Path == "" {
		return "", nil, fmt.Errorf("manifest path module %s has no recorded directory", e.Name)
	}
	dir := filepath.FromSlash(e.Path)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", nil, fmt.Errorf("manifest path module %s: %w", e.Name, err)
	}
	if !info.IsDir() {
		return "", nil, fmt.Errorf("manifest path module %s: %s is not a directory", e.Name, dir)
	}
	return dir, info, nil
}

type InstallManifest struct {
	VMake     string                 `json:"vmake"`
	Toolchain string                 `json:"toolchain"`
	Mode      string                 `json:"mode"`
	Generated string                 `json:"generated"`
	Packages  []InstallManifestEntry `json:"packages"`
}

func LoadInstallManifest(path string) (*InstallManifest, error) {
	var mf InstallManifest
	if err := jsonio.Load(path, &mf); err != nil {
		return nil, err
	}
	return &mf, nil
}

func WriteManifest(ctx *RuntimeContext, result *BuildResult, effectivePrefix string) error {
	base, err := ManifestProjectDir(ctx)
	if err != nil {
		return err
	}
	var packages []InstallManifestEntry
	gitCache := make(map[string]gitProvenance)
	recordedContainers := make(map[string]bool)

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if node == nil {
			continue
		}
		if node.IsLocal() {
			dirs := result.PkgDirs[name]
			if dirs == nil {
				continue
			}
			sourceDir := dirs.SourceDir
			relPath, err := filepath.Rel(base, sourceDir)
			if err != nil {
				return fmt.Errorf("manifest path for %s: %w", name, err)
			}
			if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
				return fmt.Errorf("local package %s source %q is outside project root %q", name, sourceDir, base)
			}
			prov := resolveGitProvenance(node.Pkg.SrcDir(), gitCache)
			entry := InstallManifestEntry{
				Name:    name,
				Version: prov.version,
				Source:  resolver.SourceKindLocal.String(),
				Ref:     prov.ref,
				Path:    filepath.ToSlash(relPath),
			}
			if node.HasOwnGit() {
				if urls := node.Pkg.GitURLs(); len(urls) > 0 {
					entry.URL = urls[0]
				}
				if node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource {
					entry.Linked = true
					entry.LinkPath = node.Source.Link.Path
					entry.Version = resolver.LocalLinkVersion
					entry.Ref = node.Source.Link.Commit
				}
			}
			packages = append(packages, entry)
			continue
		}

		// Record native container once when any of its packages participated in the build.
		if node.IsNative() {
			if result.PkgDirs[name] == nil && result.InstalledPkgs[name] == nil {
				continue
			}
			cID := node.Container
			if cID != "" && !recordedContainers[cID] {
				recordedContainers[cID] = true
				if c := ctx.DepGraph.Containers[cID]; c != nil {
					entry := InstallManifestEntry{
						Name:    cID,
						Version: c.Version,
						Source:  resolver.SourceKindNative.String(),
						URL:     c.URL,
					}
					if c.IsLinked() {
						entry.Version = resolver.LocalLinkVersion
						entry.Linked = true
						entry.LinkPath = c.Link.Path
						entry.Ref = c.Link.Commit
					} else if c.IsLocalPath() {
						entry.Version = c.Version
						entry.Ref = c.Commit
						if rel, err := filepath.Rel(base, c.Dir); err == nil {
							entry.Path = filepath.ToSlash(rel)
						} else {
							entry.Path = c.Dir
						}
					} else {
						if ref, ok := c.Versions[c.Version]; ok && ref != "" {
							entry.Ref = ref
						} else if c.Commit != "" {
							entry.Ref = c.Commit
						}
					}
					packages = append(packages, entry)
				}
			}
			// If this native member also has its own SetGit source, record it orthogonally.
			if node.HasOwnGit() {
				entry := InstallManifestEntry{
					Name:   name,
					Source: "git",
				}
				if urls := node.Pkg.GitURLs(); len(urls) > 0 {
					entry.URL = urls[0]
				}
				if node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource {
					entry.Version = resolver.LocalLinkVersion
					entry.Linked = true
					entry.LinkPath = node.Source.Link.Path
					entry.Ref = node.Source.Link.Commit
				} else {
					if locked, ok := ctx.Lock.GetSource(name); ok && locked != nil {
						entry.Version = locked.Version
						entry.Ref = locked.Commit
						if ref, ok := node.Pkg.Versions()[locked.Version]; ok && ref != "" {
							entry.Ref = ref
						}
					} else if node.Pkg != nil && node.Pkg.SrcDir() != "" {
						prov := resolveGitProvenance(node.Pkg.SrcDir(), gitCache)
						entry.Version = prov.version
						entry.Ref = prov.ref
					}
				}
				packages = append(packages, entry)
			}
			continue
		}

		// Registry package.
		ip, ok := result.InstalledPkgs[name]
		if !ok {
			continue
		}
		entry := InstallManifestEntry{
			Name:    name,
			Version: ip.Version,
			Source:  resolver.SourceKindRegistry.String(),
		}
		if node.Pkg != nil {
			if urls := node.Pkg.GitURLs(); len(urls) > 0 {
				entry.URL = urls[0]
			}
		}
		if node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource {
			entry.Linked = true
			entry.LinkPath = node.Source.Link.Path
			entry.Ref = node.Source.Link.Commit
			packages = append(packages, entry)
			continue
		}
		if node.Pkg != nil {
			if ref, ok := node.Pkg.Versions()[ip.Version]; ok {
				entry.Ref = ref
			}
		}
		packages = append(packages, entry)
	}
	mf := InstallManifest{
		VMake:     version.Version,
		Toolchain: result.TcName,
		Mode:      result.Mode,
		Generated: time.Now().UTC().Format(time.RFC3339),
		Packages:  packages,
	}
	path := filepath.Join(effectivePrefix, "manifest.json")
	return jsonio.Save(path, mf)
}

// ImportManifestIntoLock pins remote container and SetGit source versions from
// an install manifest into vmake.lock v2 BEFORE dependency resolution, so the
// graph is built from the pinned versions (not from latest-matching tags).
func ImportManifestIntoLock(ctx context.Context, projectDir, lockPath string, mf *InstallManifest) error {
	l, err := lockfile.LoadOrCreate(lockPath)
	if err != nil {
		return err
	}
	local, err := config.LoadLocal(projectDir)
	if err != nil {
		return err
	}
	changed := false
	for _, entry := range mf.Packages {
		switch entry.Source {
		case "native", "registry", "git", "local":
			if entry.isPathModule() || (entry.Source == "local" && entry.URL == "") {
				continue
			}
			if _, linked := local.Links[entry.Name]; linked {
				vlog.Info("  skip manifest entry %s (linked)", entry.Name)
				continue
			}
			ref := entry.Ref
			if ref == "" {
				ref = entry.Version
			}
			if ref == "" {
				vlog.Info("  skip manifest entry %s (empty ref)", entry.Name)
				continue
			}
			ver := entry.Version
			if ver == "" && !repo.IsCommitID(ref) {
				ver = ref
			}
			if entry.URL == "" {
				return fmt.Errorf("manifest entry %s has no URL; cannot resolve commit for %s", entry.Name, ref)
			}
			vlog.Info("  %s: resolving %s from %s", entry.Name, ref, entry.URL)
			commit, err := repo.ResolveRemoteCommitContext(ctx, entry.URL, ref)
			if err != nil {
				return fmt.Errorf("resolve commit for %s@%s: %w", entry.Name, ref, err)
			}
			if entry.Source == "native" {
				l.SetContainer(entry.Name, &lockfile.LockedContainer{
					Version: ver,
					Commit:  commit,
					Source:  "native",
				})
			} else {
				l.SetSource(entry.Name, &lockfile.LockedSource{
					Version: ver,
					Commit:  commit,
					URL:     entry.URL,
				})
			}
			vlog.Info("  pin %s -> %s (%s, commit %s)", entry.Name, lockfile.DisplayVersion(ver, commit), entry.Source, repo.ShortCommit(commit))
			changed = true
		}
	}
	if changed {
		return l.Save(lockPath)
	}
	return nil
}

// CheckoutManifestLocals checks out local packages to the refs recorded in an
// install manifest. Local packages with a managed git source are skipped: the
// manifest ref belongs to the upstream clone, while the checkout would run
// against the project repository, and the build materializes the recorded
// source version on its own.
func CheckoutManifestLocals(ctx *RuntimeContext, mf *InstallManifest) error {
	base, err := ManifestProjectDir(ctx)
	if err != nil {
		return err
	}

	for _, entry := range mf.Packages {
		if entry.isPathModule() {
			container := ctx.DepGraph.Containers[entry.Name]
			if container != nil && container.IsLinked() {
				continue
			}
			if !container.IsLocalPath() {
				return fmt.Errorf("manifest path module %s is not a path module in the project graph", entry.Name)
			}
			_, recorded, err := entry.pathModuleDir(base)
			if err != nil {
				return err
			}
			selected, err := os.Stat(container.Dir)
			if err != nil {
				return err
			}
			if !os.SameFile(recorded, selected) {
				return fmt.Errorf("manifest path %q does not match module %s directory %q", entry.Path, entry.Name, container.Dir)
			}
			continue
		}
		if entry.Source != "local" {
			continue
		}
		if entry.Ref == "" || entry.Ref == "unknown" || entry.Path == "" {
			continue
		}
		target, err := ResolveManifestEntryPath(base, entry.Path)
		if err != nil {
			return err
		}
		node := localManifestNode(ctx, entry.Name)
		if node == nil {
			return fmt.Errorf("manifest local package %q not found in the project graph", entry.Name)
		}
		if !node.IsLocal() {
			return fmt.Errorf("manifest package %q is not a local package", entry.Name)
		}
		if len(node.Pkg.GitURLs()) > 0 {
			vlog.Info("  skip checkout %s (managed git source)", entry.Name)
			continue
		}
		if node.Source == nil {
			return fmt.Errorf("manifest local package %q has no source directory", entry.Name)
		}
		sourceDir, err := filepath.Abs(node.Source.Dir)
		if err != nil {
			return fmt.Errorf("resolve manifest source for %s: %w", entry.Name, err)
		}
		if target != CanonicalInstallPath(sourceDir) {
			return fmt.Errorf("manifest path %q does not match local package %q source directory", entry.Path, entry.Name)
		}
		if err := repo.CheckoutContext(ctx.Context, target, entry.Ref); err != nil {
			return err
		}
		shortRef := repo.ShortCommit(entry.Ref)
		if shortRef != entry.Ref {
			shortRef += "..."
		}
		vlog.Info("  checkout %s -> %s", entry.Name, shortRef)
	}
	return nil
}

func CheckoutManifest(
	projectDir, depsDir string,
	sourceMgr *repo.SourceManager,
	newPkgRef func(repoName, pkgName string) *api.Package,
	mf *InstallManifest,
	filterName string,
) error {
	workspace, err := NewSourceWorkspaceFromProject(projectDir, depsDir)
	if err != nil {
		return err
	}
	found := false
	var failures []error
	for _, entry := range mf.Packages {
		if filterName != "" && entry.Name != filterName {
			continue
		}
		found = true
		if entry.Linked {
			fmt.Printf("  SKIP %s (linked to %s; run 'vmake pkg unlink %s' first)\n", entry.Name, entry.LinkPath, entry.Name)
			continue
		}

		if entry.isPathModule() {
			dir, _, err := entry.pathModuleDir(projectDir)
			if err != nil {
				failures = append(failures, err)
			} else {
				fmt.Printf("  SKIP %s (local path module at %s)\n", entry.Name, dir)
			}
			continue
		}
		switch entry.Source {
		case "local":
			if err := CheckoutManifestLocalEntry(projectDir, sourceMgr, entry); err != nil {
				return errors.Join(append(failures, err)...)
			}
		case "native", "registry", "git":
			if err := CheckoutManifestRemoteEntry(sourceMgr, workspace, newPkgRef, entry); err != nil {
				failures = append(failures, fmt.Errorf("checkout %s: %w", entry.Name, err))
			}
		default:
			fmt.Printf("  SKIP %s (unknown source: %s)\n", entry.Name, entry.Source)
		}
	}
	if filterName != "" && !found {
		return fmt.Errorf("Package '%s' not found in manifest", filterName)
	}
	return errors.Join(failures...)
}

func CheckoutManifestRemoteEntry(
	sourceMgr *repo.SourceManager,
	workspace *SourceWorkspace,
	newPkgRef func(repoName, pkgName string) *api.Package,
	entry InstallManifestEntry,
) error {
	if entry.Ref == "" {
		fmt.Printf("  SKIP %s (no ref recorded)\n", entry.Name)
		return nil
	}
	if sourceMgr == nil || workspace == nil {
		return errors.New("source manager unavailable")
	}
	repoName, pkgName, ok := api.SplitPackageRef(entry.Name)
	if !ok {
		return fmt.Errorf("invalid package reference %q", entry.Name)
	}
	var urls []string
	if newPkgRef != nil {
		pkg := newPkgRef(repoName, pkgName)
		if entry.URL != "" {
			pkg.SetGit(entry.URL)
		}
		urls = pkg.GitURLs()
	} else if entry.URL != "" {
		urls = []string{entry.URL}
	}
	req := repo.SourceRequest{
		URLs: urls,
		Ref:  entry.Ref,
	}
	source, err := workspace.Package(entry.Name)
	if err != nil {
		return err
	}
	if _, err := source.Ensure(req, sourceMgr); err != nil {
		return err
	}
	fmt.Printf("  OK   %s -> %s\n", entry.Name, entry.Ref)
	return nil
}

func CheckoutManifestLocalEntry(projectDir string, sourceMgr *repo.SourceManager, entry InstallManifestEntry) error {
	if entry.Ref == "" || entry.Ref == "unknown" {
		fmt.Printf("  SKIP %s (no ref recorded)\n", entry.Name)
		return nil
	}
	sourceDir, err := ResolveManifestEntryPath(projectDir, entry.Path)
	if err != nil {
		return fmt.Errorf("checkout %s: %w", entry.Name, err)
	}
	if !fs.FileExists(sourceDir) {
		fmt.Printf("  SKIP %s (path not found: %s)\n", entry.Name, entry.Path)
		return nil
	}
	scriptPath := filepath.Join(sourceDir, "build.go")
	if !fs.FileExists(scriptPath) {
		return fmt.Errorf("checkout %s: %s is not a package directory (build.go not found)", entry.Name, entry.Path)
	}

	ref := repo.ShortCommit(entry.Ref)
	if ref != entry.Ref {
		ref += "..."
	}

	urls, submodules, err := detectLocalManagedGit(projectDir, entry.Name, sourceDir, scriptPath)
	if err != nil {
		return fmt.Errorf("checkout %s at %s: %w", entry.Name, ref, err)
	}
	if len(urls) > 0 {
		if sourceMgr == nil {
			fmt.Printf("  SKIP %s (managed git source)\n", entry.Name)
			return nil
		}
		if info, statErr := os.Lstat(filepath.Join(sourceDir, "src")); statErr == nil && info.IsDir() {
			return fmt.Errorf("checkout %s at %s: %s already exists as a real directory; remove or move it so vmake can link the git source",
				entry.Name, ref, filepath.Join(sourceDir, "src"))
		}
		rel, _ := api.LocalRelPath(entry.Name)
		res, err := sourceMgr.EnsureSource(repo.SourceRequest{
			Key:        repo.PackageTreeKey("", build.MemberDir(rel)),
			URLs:       urls,
			Ref:        entry.Ref,
			Submodules: submodules,
		})
		if err != nil {
			return fmt.Errorf("checkout %s at %s: %w", entry.Name, ref, err)
		}
		if res != nil && res.SrcDir != "" {
			linkPath := filepath.Join(sourceDir, "src")
			if err := fs.EnsureSymlink(linkPath, res.SrcDir); err != nil {
				return fmt.Errorf("checkout %s at %s: %w", entry.Name, ref, err)
			}
		}
		fmt.Printf("  OK   %s -> %s\n", entry.Name, ref)
		return nil
	}

	if err := repo.Checkout(sourceDir, entry.Ref); err != nil {
		return fmt.Errorf("checkout %s at %s: %w", entry.Name, ref, err)
	}
	fmt.Printf("  OK   %s -> %s\n", entry.Name, ref)
	return nil
}

func detectLocalManagedGit(projectDir, name, sourceDir, scriptPath string) ([]string, bool, error) {
	restoreReadOnly := quiet.ReadOnly()
	defer func() {
		scriptfs.TakeBlockedWrites()
		restoreReadOnly()
	}()
	root, err := fs.CanonicalPath(projectDir)
	if err != nil {
		return nil, false, fmt.Errorf("resolve manifest project root: %w", err)
	}
	opts, err := buildscript.DetectScriptOptions(sourceDir, root)
	if err != nil {
		return nil, false, fmt.Errorf("script options: %w", err)
	}
	src := *buildscript.NewSource(name, scriptPath, sourceDir, api.SourceLocal)
	src.Script = opts
	pkg, err := buildscript.LoadBuildScript(src)
	if err != nil {
		if errors.Is(err, buildscript.ErrNoEntryPoint) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load build.go: %w", err)
	}
	if pkg == nil {
		return nil, false, fmt.Errorf("load build.go: empty package")
	}
	if fn := api.BindPackageGetPackageFunc(pkg); fn != nil {
		var panicErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicErr = fmt.Errorf("OnPackage panic: %v", r)
				}
			}()
			fn(pkg)
		}()
		if panicErr != nil && len(pkg.GitURLs()) == 0 {
			return nil, false, panicErr
		}
	}
	return pkg.GitURLs(), pkg.Submodules(), nil
}

func ManifestProjectDir(ctx *RuntimeContext) (string, error) {
	if ctx != nil && ctx.Paths != nil && ctx.Paths.ProjectDir != "" {
		return ctx.Paths.ProjectDir, nil
	}
	if ctx != nil && ctx.WorkDir != "" {
		return ctx.WorkDir, nil
	}
	return os.Getwd()
}

// ResolveManifestEntryPath resolves a manifest path against the project root
// and rejects paths that escape it. A path may contain ".." as long as the
// resolved location remains inside the project.
func ResolveManifestEntryPath(projectDir, entryPath string) (string, error) {
	if strings.TrimSpace(entryPath) == "" {
		return "", fmt.Errorf("manifest path is empty")
	}
	if filepath.IsAbs(entryPath) {
		return "", fmt.Errorf("manifest path %q must be relative to the project root", entryPath)
	}
	if filepath.VolumeName(entryPath) != "" {
		return "", fmt.Errorf("manifest path %q must not contain a volume", entryPath)
	}
	absolute, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("resolve project root %q: %w", projectDir, err)
	}
	base := CanonicalInstallPath(absolute)
	target := CanonicalInstallPath(filepath.Join(absolute, entryPath))
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("manifest path %q escapes the project root", entryPath)
	}
	return target, nil
}

func localManifestNode(ctx *RuntimeContext, name string) *resolver.PackageNode {
	if ctx == nil || ctx.DepGraph == nil {
		return nil
	}
	node := ctx.DepGraph.Packages[name]
	if node == nil || node.Pkg == nil {
		return nil
	}
	return node
}

type gitProvenance struct {
	version string
	ref     string
}

func resolveGitProvenance(dir string, cache map[string]gitProvenance) gitProvenance {
	key := gitRepoCacheKey(dir)
	if cached, ok := cache[key]; ok {
		return cached
	}
	ref := repo.GitRevParse(dir)
	ver := ref
	if out, err := exec.RunWithOptions("git", gitcmd.Args("describe", "--tags", "--always", "--dirty"), exec.RunOptions{Dir: dir, Quiet: true}); err == nil {
		ver = strings.TrimSpace(string(out))
	}
	res := gitProvenance{version: ver, ref: ref}
	cache[key] = res
	return res
}

func gitRepoCacheKey(dir string) string {
	canon := CanonicalInstallPath(dir)
	if canon == "" {
		return dir
	}
	for cur := canon; ; {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return canon
		}
		cur = parent
	}
}
