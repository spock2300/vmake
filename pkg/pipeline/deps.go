package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

type DepsUpdateResult struct {
	Lock    *lockfile.Lock    `json:"lock"`
	Changes []lockfile.Change `json:"changes"`
	DryRun  bool              `json:"dryRun"`
}

type DepsSyncOptions struct {
	DryRun bool
	Prune  bool
}

func SyncOrUpdateDeps(ctx *RuntimeContext, dryRun bool) (*DepsUpdateResult, error) {
	return SyncOrUpdateDepsWithOptions(ctx, DepsSyncOptions{DryRun: dryRun})
}

func SyncOrUpdateDepsWithOptions(ctx *RuntimeContext, opts DepsSyncOptions) (_ *DepsUpdateResult, err error) {
	defer scriptcall.Recover(&err)
	dryRun := opts.DryRun
	needed, err := resolveNeeded(ctx, true, true)
	if err != nil {
		return nil, err
	}
	containerQuery := ctx.lockQuery().Containers()
	sourceQuery := ctx.lockQuery().Sources()
	for cID := range ctx.DepGraph.Containers {
		if containerQuery.NeedsSync(cID) {
			for pkgName, node := range ctx.DepGraph.Packages {
				if node != nil && node.Container == cID {
					needed[pkgName] = true
				}
			}
		}
	}
	for name := range ctx.DepGraph.Packages {
		if sourceQuery.NeedsSync(name) {
			needed[name] = true
		}
	}
	rc := newRunContext(ctx, BuildOptions{}, nil)
	conf, err := prepareConfig(rc, needed, nil)
	if err != nil {
		return nil, err
	}

	src := newSourceStage(rc, conf)
	src.pruneLock = opts.Prune
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
	if !dryRun {
		if err := src.applyPatchesToNeeded(); err != nil {
			return nil, err
		}
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
	Section      string   `json:"section,omitempty"`
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
	needed, err := resolveNeeded(ctx, true, true)
	if err != nil {
		return nil, err
	}
	_, statErr := os.Stat(ctx.LockPath)
	lockExists := statErr == nil
	sourceMgr := repo.NewSourceManager(ctx.Paths.DepsDir, ctx.Paths.CacheDir).WithSession(ctx.Locks).WithContext(ctx.Context)
	neededContainers := NeededContainers(ctx.DepGraph, needed)

	seenContainers := make(map[string]bool)
	seenSources := make(map[string]bool)
	var packages []DepPackageStatus

	// 1. External containers (native & registry).
	containerIDs := make([]string, 0, len(ctx.DepGraph.Containers))
	for id := range ctx.DepGraph.Containers {
		containerIDs = append(containerIDs, id)
	}
	sort.Strings(containerIDs)
	for _, id := range containerIDs {
		c := ctx.DepGraph.Containers[id]
		if c == nil || c.IsLocal() {
			continue
		}
		seenContainers[id] = true
		if !neededContainers[id] {
			continue
		}
		constraints := append([]string(nil), c.Constraints...)
		if constraints == nil {
			constraints = []string{}
		}
		pinned := ""
		if ctx.Pins != nil {
			pinned = ctx.Pins[id]
		}
		linked := ""
		if ctx.SourceLinks != nil {
			linked = ctx.SourceLinks[id]
		}
		if c.IsLocalPath() {
			packages = append(packages, DepPackageStatus{
				Section:      string(resolver.LockSectionContainer),
				Name:         id,
				Source:       "path",
				Version:      c.Version,
				Commit:       c.Commit,
				Constraints:  constraints,
				Linked:       c.Dir,
				Dirty:        repo.IsDirtyContext(ctx.Context, c.Dir),
				Materialized: true,
				Cached:       false,
				State:        "path",
			})
			continue
		}
		if linked != "" {
			commit := c.Commit
			if c.Link != nil && c.Link.Commit != "" {
				commit = c.Link.Commit
			}
			packages = append(packages, DepPackageStatus{
				Section:      string(resolver.LockSectionContainer),
				Name:         id,
				Source:       c.Kind.String(),
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

		var locked *lockfile.LockedContainer
		hasLock := false
		if ctx.Lock != nil {
			locked, hasLock = ctx.Lock.GetContainer(id)
		}

		if c.IsRegistry() {
			head := c.Commit
			if head == "" && ctx.Paths != nil && ctx.Paths.ReposDir != "" {
				mgr := repo.NewRepoManager(ctx.Paths.ReposDir)
				if mgr.Exists(c.Repo) {
					head, _ = repo.GetCurrentCommitContext(ctx.Context, mgr.Path(c.Repo))
				}
			}
			commit := head
			if hasLock && locked.Commit != "" {
				commit = locked.Commit
			}
			wrapperOutdated := hasLock && locked.Commit != "" && head != "" && head != locked.Commit
			state := "locked"
			if !hasLock {
				state = "unlocked"
			} else if wrapperOutdated {
				state = "wrapper_outdated"
			}
			packages = append(packages, DepPackageStatus{
				Section:      string(resolver.LockSectionContainer),
				Name:         id,
				Source:       c.Kind.String(),
				Version:      "",
				Commit:       commit,
				Constraints:  constraints,
				Materialized: true,
				Cached:       false,
				State:        state,
			})
			continue
		}

		// Native container.
		query := resolver.LockQuery{Pins: ctx.Pins, Lock: ctx.Lock, Section: resolver.LockSectionContainer}
		decision := query.Decide(id, c.Version, c.Commit)
		version, commit := decision.Version, decision.Commit
		if decision.Provenance == resolver.ProvenanceTargetCommit {
			version = ""
		}
		pinMismatch := false
		if problem, ok := query.PinLockMismatch(id); ok {
			pinMismatch = problem.Pin != ""
		}

		root := sourceMgr.TreeDir(id)
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
		if c.URL != "" {
			urls = []string{c.URL}
		}
		cached := sourceMgr.HasCachedCommit(urls, commit)

		state := "locked"
		if !hasLock {
			state = "unlocked"
		} else if pinMismatch {
			state = "pin_mismatch"
		} else if !materialized {
			state = "missing"
		} else if pinned != "" {
			state = "pinned"
		}
		packages = append(packages, DepPackageStatus{
			Section:      string(resolver.LockSectionContainer),
			Name:         id,
			Source:       c.Kind.String(),
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

	// 2. Independent SetGit sources (local, registry, native members).
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || !node.HasOwnGit() {
			continue
		}
		seenSources[name] = true
		if !needed[name] {
			continue
		}

		constraints := append([]string(nil), node.Constraints...)
		if constraints == nil {
			constraints = []string{}
		}
		pinned := ""
		if ctx.Pins != nil {
			pinned = ctx.Pins[name]
		}
		loc := ctx.packageLocation(name, node)
		if loc.Linked && node.Source != nil && node.Source.Link != nil {
			linked := node.Source.Link.Path
			packages = append(packages, DepPackageStatus{
				Section:      string(resolver.LockSectionSource),
				Name:         name,
				Source:       "git",
				Version:      resolver.LocalLinkVersion,
				Commit:       node.Source.Link.Commit,
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

		hasLock := false
		if ctx.Lock != nil {
			_, hasLock = ctx.Lock.GetSource(name)
		}
		selected := ""
		if len(node.Pkg.Versions()) > 0 {
			_, semver, _ := resolver.SplitConstraints(name, node.Constraints)
			if len(semver) > 0 {
				selected, _ = node.Pkg.SelectVersionMulti(semver)
			} else {
				selected, _ = node.Pkg.SelectVersion("")
			}
		}
		query := resolver.LockQuery{Pins: ctx.Pins, Lock: ctx.Lock, Section: resolver.LockSectionSource}
		decision := query.Decide(name, selected, "")
		version, commit := decision.Version, decision.Commit
		if decision.Provenance == resolver.ProvenanceTargetCommit {
			version = ""
		}
		pinMismatch := false
		if problem, ok := query.PinLockMismatch(name); ok {
			pinMismatch = problem.Pin != ""
		}

		treeCommit := sourceMgr.CommitAt(loc.TreeRoot)
		materialized := false
		if commit != "" {
			materialized = treeCommit != "" && treeCommit == commit
		} else if !pinMismatch && treeCommit != "" && version == "" {
			commit = treeCommit
			materialized = true
		}
		dirty := materialized && repo.IsDirtyContext(ctx.Context, filepath.Join(loc.TreeRoot, "src"))
		cached := sourceMgr.HasCachedCommit(node.Pkg.GitURLs(), commit)

		state := "locked"
		if !hasLock {
			state = "unlocked"
		} else if pinMismatch {
			state = "pin_mismatch"
		} else if !materialized {
			state = "missing"
		} else if pinned != "" {
			state = "pinned"
		}

		packages = append(packages, DepPackageStatus{
			Section:      string(resolver.LockSectionSource),
			Name:         name,
			Source:       "git",
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
		for _, name := range ctx.Lock.SortedContainerNames() {
			if !seenContainers[name] {
				if ctx.SourceLinks != nil && ctx.SourceLinks[name] != "" {
					continue
				}
				stale = append(stale, name)
			}
		}
		for _, name := range ctx.Lock.SortedSourceNames() {
			if !seenSources[name] {
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
	Section string `json:"section,omitempty"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	SrcDir  string `json:"srcDir"`
}

type DepsFetchResult struct {
	Packages []FetchedPackage `json:"packages"`
}

// FetchDeps materializes locked dependency containers and SetGit sources into
// the project tree.
func FetchDeps(ctx *RuntimeContext, pkgs []string, all bool, offline bool, onFetched func([]FetchedPackage)) (_ *DepsFetchResult, err error) {
	defer scriptcall.Recover(&err)
	var needed NeededSet
	if all {
		needed = make(map[string]bool, len(ctx.DepGraph.Packages))
		for name := range ctx.DepGraph.Packages {
			needed[name] = true
		}
	} else {
		needed, err = resolveNeeded(ctx, true, true)
		if err != nil {
			return nil, err
		}
	}
	if len(pkgs) > 0 {
		selected := make(map[string]bool)
		for _, want := range pkgs {
			if node := ctx.DepGraph.Packages[want]; node != nil {
				selected[want] = true
				continue
			}
			if c := ctx.DepGraph.Containers[want]; c != nil {
				matched := false
				for name, node := range ctx.DepGraph.Packages {
					if node != nil && node.Container == want && needed[name] {
						selected[name] = true
						matched = true
					}
				}
				if !matched {
					for name, node := range ctx.DepGraph.Packages {
						if node != nil && node.Container == want {
							selected[name] = true
							matched = true
						}
					}
				}
				if matched {
					continue
				}
			}
			return nil, fmt.Errorf("package or container %q is not in the dependency graph", want)
		}
		needed = selected
	}

	rc := newRunContext(ctx, BuildOptions{}, nil)
	conf, err := prepareConfig(rc, needed, nil)
	if err != nil {
		return nil, err
	}
	src := newSourceStage(rc, conf)
	src.offline = offline
	src.locked = true
	if err := src.prepareAllPackages(); err != nil {
		return nil, err
	}

	var fetched []FetchedPackage
	neededContainers := NeededContainers(ctx.DepGraph, needed)
	containerIDs := make([]string, 0, len(neededContainers))
	for id := range neededContainers {
		containerIDs = append(containerIDs, id)
	}
	sort.Strings(containerIDs)
	for _, id := range containerIDs {
		c := ctx.DepGraph.Containers[id]
		if c == nil || !c.IsNative() || c.IsLinked() || c.IsLocalPath() {
			continue
		}
		tree := src.containerTrees[id]
		srcDir := ""
		if tree != "" {
			srcDir = filepath.Join(tree, "src")
		}
		fetched = append(fetched, FetchedPackage{
			Section: string(resolver.LockSectionContainer),
			Name:    id,
			Version: src.containerVersions[id],
			Commit:  src.containerCommits[id],
			SrcDir:  srcDir,
		})
	}
	for _, name := range ctx.Resolver.GetOrder() {
		if !needed[name] {
			continue
		}
		node := ctx.DepGraph.Packages[name]
		if node == nil || !node.HasOwnGit() {
			continue
		}
		if node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource {
			continue
		}
		srcDir := src.sourceSeeds[name]
		if srcDir == "" && node.Pkg != nil {
			srcDir = node.Pkg.SrcDir()
		}
		fetched = append(fetched, FetchedPackage{
			Section: string(resolver.LockSectionSource),
			Name:    name,
			Version: src.sourceVersions[name],
			Commit:  src.sourceCommits[name],
			SrcDir:  srcDir,
		})
	}

	if onFetched != nil {
		onFetched(fetched)
	}

	if err := src.applyPatchesToNeeded(); err != nil {
		return nil, err
	}

	sort.Slice(fetched, func(i, j int) bool {
		if fetched[i].Section != fetched[j].Section {
			return fetched[i].Section < fetched[j].Section
		}
		return fetched[i].Name < fetched[j].Name
	})
	if fetched == nil {
		fetched = []FetchedPackage{}
	}
	return &DepsFetchResult{Packages: fetched}, nil
}
