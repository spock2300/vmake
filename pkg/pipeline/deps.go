package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

type DepsUpdateResult struct {
	Lock    *lockfile.Lock    `json:"lock"`
	Changes []lockfile.Change `json:"changes"`
	DryRun  bool              `json:"dryRun"`
}

func SyncOrUpdateDeps(ctx *RuntimeContext, dryRun bool) (_ *DepsUpdateResult, err error) {
	defer scriptcall.Recover(&err)
	needed, err := filterAndCollectNeeded(ctx)
	if err != nil {
		return nil, err
	}
	if err := resolveActiveChip(ctx, needed); err != nil {
		return nil, err
	}
	subParents := ctx.Resolver.SubParents()
	for name := range ctx.DepGraph.Packages {
		_, explicitTarget := ctx.LockPolicy.Targets[name]
		explicitFloat := ctx.LockPolicy.Float != nil && ctx.LockPolicy.Float[name]
		pin := ""
		if ctx.Pins != nil {
			pin = strings.TrimSpace(ctx.Pins[name])
		}
		pinNeedsSync := false
		if pin != "" {
			if ctx.Lock == nil {
				pinNeedsSync = true
			} else if locked, ok := ctx.Lock.Get(name); !ok ||
				(repo.IsCommitID(pin) && locked.Commit != pin) ||
				(!repo.IsCommitID(pin) && locked.Version != pin) {
				pinNeedsSync = true
			}
		}
		if explicitTarget || explicitFloat || pinNeedsSync {
			needed[name] = true
			if parent := subParents[name]; parent != "" {
				needed[parent] = true
			}
		}
	}
	rc := newRunContext(ctx, BuildOptions{}, nil)
	conf, err := prepareConfig(rc, needed)
	if err != nil {
		return nil, err
	}

	src := newSourceStage(rc, conf)
	if dryRun {
		tmpDir, err := os.MkdirTemp("", "vmake-deps-dryrun-*")
		if err != nil {
			return nil, fmt.Errorf("create dry-run temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)
		src.dryRun = true
		src.dryRunDepsDir = tmpDir
	}
	if err := src.prepareAllPackages(); err != nil {
		return nil, err
	}
	if err := src.applyPatchesToNeeded(); err != nil {
		return nil, err
	}
	updated, err := src.buildLockfile()
	if err != nil {
		return nil, err
	}
	changes := lockfile.Diff(ctx.Lock, updated)
	if changes == nil {
		changes = []lockfile.Change{}
	}
	if !dryRun {
		if err := src.saveLockfile(updated); err != nil {
			return nil, err
		}
	}
	return &DepsUpdateResult{
		Lock:    updated,
		Changes: changes,
		DryRun:  dryRun,
	}, nil
}

type DepPackageStatus struct {
	Name         string   `json:"name"`
	Source       string   `json:"source"`
	Version      string   `json:"version"`
	Commit       string   `json:"commit"`
	Constraints  []string `json:"constraints"`
	Pinned       string   `json:"pinned,omitempty"`
	Linked       string   `json:"linked,omitempty"`
	Dirty        bool     `json:"dirty"`
	Materialized bool     `json:"materialized"`
	Cached       bool     `json:"cached"`
	State        string   `json:"state"`
}

type DepsStatusReport struct {
	LockPath         string             `json:"lockPath"`
	LockExists       bool               `json:"lockExists"`
	Packages         []DepPackageStatus `json:"packages"`
	StaleLockEntries []string           `json:"staleLockEntries,omitempty"`
}

func InspectDepsStatus(ctx *RuntimeContext) (_ *DepsStatusReport, err error) {
	defer scriptcall.Recover(&err)
	needed, err := filterAndCollectNeeded(ctx)
	if err != nil {
		return nil, err
	}
	_, statErr := os.Stat(ctx.LockPath)
	lockExists := statErr == nil
	sourceMgr := repo.NewSourceManager(ctx.Paths.DepsDir, ctx.Paths.CacheDir).WithSession(ctx.Locks).WithContext(ctx.Context)
	subParents := ctx.Resolver.SubParents()

	seenInGraph := make(map[string]bool)
	var packages []DepPackageStatus
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil {
			continue
		}
		if _, isSub := subParents[name]; isSub {
			continue
		}
		if node.IsLocal() && (node.Pkg == nil || len(node.Pkg.GitURLs()) == 0) {
			continue
		}
		seenInGraph[name] = true
		if !needed[name] {
			continue
		}

		sourceKind := "registry"
		if node.IsLocal() {
			sourceKind = "git"
		} else if node.IsNative() {
			sourceKind = "native"
		}

		constraints := make([]string, 0, len(node.Constraints))
		constraints = append(constraints, node.Constraints...)

		pinned := ""
		if ctx.Pins != nil {
			pinned = ctx.Pins[name]
		}
		linked := ""
		if ctx.SourceLinks != nil {
			linked = ctx.SourceLinks[name]
		}

		if linked != "" {
			commit := ""
			if node.Source != nil && node.Source.Link != nil {
				commit = node.Source.Link.Commit
			}
			packages = append(packages, DepPackageStatus{
				Name:         name,
				Source:       sourceKind,
				Version:      resolver.LocalLinkVersion,
				Commit:       commit,
				Constraints:  constraints,
				Pinned:       pinned,
				Linked:       linked,
				Dirty:        repo.IsDirtyContext(ctx.Context, linked),
				Materialized: true,
				Cached:       false,
				State:        "linked",
			})
			continue
		}

		var locked *lockfile.LockedPkg
		hasLock := false
		if ctx.Lock != nil {
			locked, hasLock = ctx.Lock.Get(name)
		}

		version := ""
		commit := ""
		if hasLock {
			version = locked.Version
			commit = locked.Commit
		}
		pinMismatch := false
		if pinned != "" {
			if repo.IsCommitID(pinned) {
				pinMismatch = !hasLock || locked.Commit != pinned
				version = ""
				commit = pinned
			} else {
				pinMismatch = !hasLock || locked.Version != pinned
				version = pinned
				if pinMismatch {
					commit = ""
				}
			}
		} else if !hasLock {
			if node.Native != nil {
				version = node.Native.Selected
				commit = node.Native.Commit
			} else if node.Pkg != nil && len(node.Pkg.Versions()) > 0 {
				if len(node.Constraints) > 0 {
					version, _ = node.Pkg.SelectVersionMulti(node.Constraints)
				} else {
					version, _ = node.Pkg.SelectVersion("")
				}
			}
		}

		var root string
		if node.IsLocal() {
			root = sourceMgr.TreeDir(repo.PackageTreeKey("", name))
		} else {
			root = sourceMgr.TreeDir(name)
		}
		treeCommit := sourceMgr.CommitAt(root)
		materialized := false
		if commit != "" {
			materialized = treeCommit != "" && treeCommit == commit
		} else if !pinMismatch && treeCommit != "" && version == "" {
			commit = treeCommit
			materialized = true
		}
		dirty := materialized && repo.IsDirtyContext(ctx.Context, filepath.Join(root, "src"))

		var urls []string
		if node.Native != nil && node.Native.GitURL != "" {
			urls = []string{node.Native.GitURL}
		} else if node.Pkg != nil {
			urls = node.Pkg.GitURLs()
		}
		cached := sourceMgr.HasCachedCommit(urls, commit)

		wrapperOutdated := false
		if sourceKind == "registry" && hasLock && locked.WrapperCommit != "" && ctx.Paths != nil && ctx.Paths.ReposDir != "" {
			if repoName, _, ok := api.SplitPackageRef(name); ok {
				mgr := repo.NewRepoManager(ctx.Paths.ReposDir)
				if mgr.Exists(repoName) {
					if head, err := repo.GetCurrentCommitContext(ctx.Context, mgr.Path(repoName)); err == nil && head != "" && head != locked.WrapperCommit {
						wrapperOutdated = true
					}
				}
			}
		}

		state := "locked"
		if !hasLock {
			state = "unlocked"
		} else if pinMismatch {
			state = "pin_mismatch"
		} else if wrapperOutdated {
			state = "wrapper_outdated"
		} else if !materialized {
			state = "missing"
		} else if pinned != "" {
			state = "pinned"
		}

		packages = append(packages, DepPackageStatus{
			Name:         name,
			Source:       sourceKind,
			Version:      version,
			Commit:       commit,
			Constraints:  constraints,
			Pinned:       pinned,
			Dirty:        dirty,
			Materialized: materialized,
			Cached:       cached,
			State:        state,
		})
	}

	var stale []string
	if ctx.Lock != nil {
		for _, name := range ctx.Lock.SortedNames() {
			if !seenInGraph[name] {
				if ctx.SourceLinks != nil && ctx.SourceLinks[name] != "" {
					continue
				}
				stale = append(stale, name)
			}
		}
	}
	if packages == nil {
		packages = []DepPackageStatus{}
	}
	return &DepsStatusReport{
		LockPath:         ctx.LockPath,
		LockExists:       lockExists,
		Packages:         packages,
		StaleLockEntries: stale,
	}, nil
}

type FetchedPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	SrcDir  string `json:"srcDir"`
}

type DepsFetchResult struct {
	Packages []FetchedPackage `json:"packages"`
}

func FetchDeps(ctx *RuntimeContext, pkgs []string, all bool, offline bool) (_ *DepsFetchResult, err error) {
	defer scriptcall.Recover(&err)
	var needed map[string]bool
	if all {
		needed = make(map[string]bool, len(ctx.DepGraph.Packages))
		for name := range ctx.DepGraph.Packages {
			needed[name] = true
		}
	} else {
		needed, err = filterAndCollectNeeded(ctx)
		if err != nil {
			return nil, err
		}
	}
	if len(pkgs) > 0 {
		subParents := ctx.Resolver.SubParents()
		selected := make(map[string]bool)
		for _, want := range pkgs {
			node := ctx.DepGraph.Packages[want]
			if node == nil {
				return nil, fmt.Errorf("package %q is not in the dependency graph", want)
			}
			selected[want] = true
			if parent := subParents[want]; parent != "" {
				selected[parent] = true
			}
			for name, parent := range subParents {
				if parent == want {
					subNode := ctx.DepGraph.Packages[name]
					if needed[name] || (subNode != nil && (subNode.Pkg == nil || len(subNode.Pkg.GitURLs()) == 0)) {
						selected[name] = true
					}
				}
			}
		}
		needed = selected
	}

	rc := newRunContext(ctx, BuildOptions{}, nil)
	conf, err := prepareConfig(rc, needed)
	if err != nil {
		return nil, err
	}
	src := newSourceStage(rc, conf)
	src.offline = offline
	src.locked = true
	if err := src.prepareAllPackages(); err != nil {
		return nil, err
	}
	if err := src.applyPatchesToNeeded(); err != nil {
		return nil, err
	}

	subParents := ctx.Resolver.SubParents()
	var fetched []FetchedPackage
	for _, name := range ctx.Resolver.GetOrder() {
		if !needed[name] {
			continue
		}
		node := ctx.DepGraph.Packages[name]
		if node == nil {
			continue
		}
		if node.Source != nil && node.Source.Link != nil {
			continue
		}
		if _, isSub := subParents[name]; isSub {
			continue
		}
		if node.IsLocal() && (node.Pkg == nil || len(node.Pkg.GitURLs()) == 0) {
			continue
		}
		commit := src.sourceCommits[name]
		if commit == "" && src.remote != nil {
			commit = src.remote.commits[name]
		}
		srcDir := ""
		if node.Pkg != nil && node.Pkg.SrcDir() != "" {
			srcDir = node.Pkg.SrcDir()
		} else if dirs := src.pkgDirs[name]; dirs != nil {
			srcDir = dirs.SourceDir
		}
		fetched = append(fetched, FetchedPackage{
			Name:    name,
			Version: src.sourceVersions[name],
			Commit:  commit,
			SrcDir:  srcDir,
		})
	}
	sort.Slice(fetched, func(i, j int) bool { return fetched[i].Name < fetched[j].Name })
	if fetched == nil {
		fetched = []FetchedPackage{}
	}
	return &DepsFetchResult{Packages: fetched}, nil
}
