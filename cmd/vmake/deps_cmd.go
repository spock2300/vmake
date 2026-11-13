package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

func newDepsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deps",
		Short: "Manage project dependencies, graph, pins, working trees and .vmake/vmake.lock",
		Long: `Manage and inspect project dependencies across build.go constraints, unified
package/target dependency graphs, .vmake/project.json pins, .vmake/vmake.lock,
.vmake_deps/ working trees and ~/.vmake/cache/git/.`,
	}
	addPersistentConfigFlag(cmd)
	cmd.AddCommand(
		newDepsStatusCmd(),
		newDepsGraphCmd(),
		newDepsWhyCmd(),
		newDepsCheckCmd(),
		newDepsSyncCmd(),
		newDepsUpdateCmd(),
		newDepsPinCmd(),
		newDepsUnpinCmd(),
		newDepsFetchCmd(),
	)
	return cmd
}

type depsStatusEnvelope struct {
	SchemaVersion    int                         `json:"schemaVersion"`
	Command          string                      `json:"command"`
	Project          string                      `json:"project"`
	ConfigFile       string                      `json:"configFile"`
	Lockfile         string                      `json:"lockfile"`
	LockExists       bool                        `json:"lockExists"`
	Packages         []pipeline.DepPackageStatus `json:"packages"`
	StaleLockEntries []string                    `json:"staleLockEntries,omitempty"`
}

type depsUpdateEnvelope struct {
	SchemaVersion int                            `json:"schemaVersion"`
	Command       string                         `json:"command"`
	DryRun        bool                           `json:"dryRun"`
	Changes       []lockfile.Change              `json:"changes"`
	Packages      map[string]*lockfile.LockedPkg `json:"packages"`
}

type depsFetchEnvelope struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Command       string                    `json:"command"`
	Offline       bool                      `json:"offline"`
	Packages      []pipeline.FetchedPackage `json:"packages"`
}

func newDepsStatusCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show unified dependency state across lock, pins, working trees and cache",
		Long: `Show the resolved dependency status for the active configuration, including
locked version/commit, project-level pin (.vmake/project.json), local link
(.vmake/local.json), .vmake_deps/ working tree state and ~/.vmake/cache/git/
availability.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				vlog.SetLevel(vlog.Quiet)
			}
			policy := resolver.LockPolicy{DryRun: true}
			ctx, err := openProject(projectOptions{execution: cmd.Context(), policy: &policy})
			if err != nil {
				return err
			}
			defer closeProject(ctx)
			report, err := pipeline.InspectDepsStatus(ctx)
			if err != nil {
				return err
			}
			if jsonOut {
				configFile := filepath.Base(ctx.ConfigPath)
				if rel, err := filepath.Rel(ctx.Paths.ProjectDir, ctx.ConfigPath); err == nil {
					configFile = filepath.ToSlash(rel)
				}
				return writeDepsJSON(cmd.OutOrStdout(), &depsStatusEnvelope{
					SchemaVersion:    1,
					Command:          "deps status",
					Project:          ctx.Paths.ProjectDir,
					ConfigFile:       configFile,
					Lockfile:         report.LockPath,
					LockExists:       report.LockExists,
					Packages:         report.Packages,
					StaleLockEntries: report.StaleLockEntries,
				})
			}
			return printDepsStatusTable(cmd.OutOrStdout(), report)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print machine-readable JSON")
	return cmd
}

func newDepsSyncCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Synchronize working trees and vmake.lock without floating locked packages",
		Long: `Reconcile .vmake_deps/ working trees and .vmake/vmake.lock with build.go
requirements and .vmake/project.json pins.

Already-locked packages that satisfy current constraints and pins keep their
locked commit; newly added or newly re-pinned packages are resolved, fetched
and recorded in .vmake/vmake.lock, and stale lock entries are pruned.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				vlog.SetLevel(vlog.Quiet)
			}
			policy := resolver.LockPolicy{}
			ctx, err := openProject(projectOptions{execution: cmd.Context(), policy: &policy})
			if err != nil {
				return err
			}
			defer closeProject(ctx)
			res, err := pipeline.SyncOrUpdateDeps(ctx, false)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeDepsUpdateJSON(cmd.OutOrStdout(), "deps sync", res)
			}
			printDepsUpdateSummary(cmd.OutOrStdout(), "Synchronized", res, ctx.SourceLinks)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print machine-readable JSON")
	return cmd
}

func newDepsUpdateCmd() *cobra.Command {
	var dryRun bool
	var refreshRepos bool
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "update [pkg[@version|commit]...]",
		Short: "Re-resolve dependencies and rewrite vmake.lock",
		Long: `Update all or selected project dependencies, materialize the selected commits
into .vmake_deps/ and rewrite .vmake/vmake.lock.

Without arguments, all unlocked/unpinned dependencies re-resolve to the latest
version matching build.go constraints (packages pinned in .vmake/project.json
stay at their pin; use 'vmake deps pin' or 'vmake deps unpin' to change a
pinned package). With package arguments, only the named packages are updated
while other dependencies remain locked. Passing <pkg>@<version|commit> updates
that package to the specified version or commit in .vmake/vmake.lock without
writing a persistent pin to .vmake/project.json.`,
		ValidArgsFunction: completeDepsPkgRef,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				vlog.SetLevel(vlog.Quiet)
			}
			if refreshRepos && dryRun {
				return &usageError{err: fmt.Errorf("--refresh-repos cannot be combined with --dry-run")}
			}
			projectDir := findProjectDir()
			local, err := config.LoadLocal(projectDir)
			if err != nil {
				return err
			}
			pins, err := config.LoadProjectPins(projectDir)
			if err != nil {
				return err
			}
			policy, requested, err := parseDepsUpdateArgs(args, local.Links, pins, dryRun)
			if err != nil {
				return err
			}
			if refreshRepos {
				if err := refreshRegistryReposForUpdate(requested); err != nil {
					return err
				}
			}
			ctx, err := openProject(projectOptions{execution: cmd.Context(), policy: &policy})
			if err != nil {
				return err
			}
			defer closeProject(ctx)
			if err := validateRequestedPackagesInGraph(ctx, requested); err != nil {
				return err
			}
			res, err := pipeline.SyncOrUpdateDeps(ctx, dryRun)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeDepsUpdateJSON(cmd.OutOrStdout(), "deps update", res)
			}
			action := "Updated"
			if dryRun {
				action = "Would update"
			}
			printDepsUpdateSummary(cmd.OutOrStdout(), action, res, ctx.SourceLinks)
			if !dryRun && len(res.Changes) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), trustKeptNotice)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview lockfile changes without modifying .vmake_deps/ or vmake.lock")
	cmd.Flags().BoolVar(&refreshRepos, "refresh-repos", false, "git pull registered package repositories before resolving")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print machine-readable JSON")
	return cmd
}

func newDepsPinCmd() *cobra.Command {
	var dryRun bool
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "pin <pkg>@<version|commit>",
		Short: "Pin a dependency version or commit in .vmake/project.json and vmake.lock",
		Long: `Record a project-wide dependency pin in .vmake/project.json, materialize the
selected version or commit in .vmake_deps/ and update .vmake/vmake.lock.

Project pins are shared by all configurations in configs/ and prevent bare
'vmake deps update' from floating the pinned package.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeDepsPkgRef,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				vlog.SetLevel(vlog.Quiet)
			}
			pkgName, target, hasTarget, err := splitDepTargetArg(args[0])
			if err != nil {
				return &usageError{err: err}
			}
			if !hasTarget {
				return &usageError{err: fmt.Errorf("pin requires <pkg>@<version|commit>, got %q", args[0])}
			}
			projectDir := findProjectDir()
			local, err := config.LoadLocal(projectDir)
			if err != nil {
				return err
			}
			if err := validateUpdatablePkgRef(pkgName, local.Links); err != nil {
				return err
			}
			pins, err := config.LoadProjectPins(projectDir)
			if err != nil {
				return err
			}
			updatedPins := maps.Clone(pins)
			if updatedPins == nil {
				updatedPins = make(map[string]string)
			}
			updatedPins[pkgName] = target

			policy := resolver.LockPolicy{
				Targets: map[string]string{pkgName: target},
				DryRun:  dryRun,
			}
			ctx, err := openProject(projectOptions{execution: cmd.Context(), policy: &policy, pins: updatedPins})
			if err != nil {
				return err
			}
			defer closeProject(ctx)
			if err := validateRequestedPackagesInGraph(ctx, []string{pkgName}); err != nil {
				return err
			}
			res, err := pipeline.SyncOrUpdateDeps(ctx, dryRun)
			if err != nil {
				return err
			}
			if !dryRun {
				if err := config.SaveProjectPins(projectDir, updatedPins); err != nil {
					return err
				}
			}
			if jsonOut {
				return writeDepsUpdateJSON(cmd.OutOrStdout(), "deps pin", res)
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Would pin %s@%s in .vmake/project.json\n", pkgName, target)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Pinned %s@%s in .vmake/project.json\n", pkgName, target)
			}
			printDepsChanges(cmd.OutOrStdout(), res.Changes)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview pin and lockfile changes without writing files")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print machine-readable JSON")
	return cmd
}

func newDepsUnpinCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "unpin <pkg>",
		Short: "Remove a dependency pin from .vmake/project.json",
		Long: `Remove a project-wide dependency pin from .vmake/project.json.
The currently locked version in .vmake/vmake.lock is kept as long as it
satisfies build.go constraints.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeDepsPkgRef,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				vlog.SetLevel(vlog.Quiet)
			}
			pkgName := strings.TrimSpace(args[0])
			if pkgName == "" || strings.Contains(pkgName, "@") {
				return &usageError{err: fmt.Errorf("invalid package name %q", args[0])}
			}
			commandStorageLocks()
			projectDir := findProjectDir()
			pins, err := config.LoadProjectPins(projectDir)
			if err != nil {
				return err
			}
			if _, ok := pins[pkgName]; !ok {
				return fmt.Errorf("package %s is not pinned in .vmake/project.json", pkgName)
			}
			delete(pins, pkgName)
			if err := config.SaveProjectPins(projectDir, pins); err != nil {
				return err
			}
			if jsonOut {
				lock, err := lockfile.LoadOrCreate(getLockfilePath())
				if err != nil {
					return err
				}
				return writeDepsUpdateJSON(cmd.OutOrStdout(), "deps unpin", &pipeline.DepsUpdateResult{
					Lock:    lock,
					Changes: []lockfile.Change{},
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Unpinned %s from .vmake/project.json\n", pkgName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print machine-readable JSON")
	return cmd
}

func newDepsFetchCmd() *cobra.Command {
	var fetchAll bool
	var offline bool
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "fetch [pkg...]",
		Short: "Materialize locked dependency sources into .vmake_deps/ without building",
		Long: `Populate ~/.vmake/cache/git/ and materialize .vmake_deps/ working trees at
the versions/commits recorded in .vmake/vmake.lock without running a build.

Use --offline to materialize working trees strictly from ~/.vmake/cache/git/
without contacting upstream git remotes.`,
		ValidArgsFunction: completeDepsPkgRef,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				vlog.SetLevel(vlog.Quiet)
			}
			if fetchAll && len(args) > 0 {
				return &usageError{err: fmt.Errorf("cannot combine package arguments with --all")}
			}
			policy := resolver.LockPolicy{
				Locked:        true,
				StrictWrapper: true,
				Offline:       offline,
			}
			ctx, err := openProject(projectOptions{execution: cmd.Context(), policy: &policy})
			if err != nil {
				return err
			}
			defer closeProject(ctx)
			if err := validateRequestedPackagesInGraph(ctx, args); err != nil {
				return err
			}
			var report func([]pipeline.FetchedPackage)
			if !jsonOut {
				report = func(packages []pipeline.FetchedPackage) {
					if len(packages) == 0 {
						fmt.Fprintln(cmd.OutOrStdout(), "No dependency sources to fetch.")
						return
					}
					for _, p := range packages {
						ver := lockfile.DisplayVersion(p.Version, p.Commit)
						fmt.Fprintf(cmd.OutOrStdout(), "Fetched %s %s (%s)\n", p.Name, ver, repo.ShortCommit(p.Commit))
					}
				}
			}
			res, err := pipeline.FetchDeps(ctx, args, fetchAll, offline, report)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeDepsJSON(cmd.OutOrStdout(), &depsFetchEnvelope{
					SchemaVersion: 1,
					Command:       "deps fetch",
					Offline:       offline,
					Packages:      res.Packages,
				})
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fetchAll, "all", false, "fetch all resolved dependencies regardless of active conditional options")
	cmd.Flags().BoolVar(&offline, "offline", false, "materialize strictly from ~/.vmake/cache/git/ without network access")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print machine-readable JSON")
	return cmd
}

func parseDepsUpdateArgs(args []string, links map[string]string, pins map[string]string, dryRun bool) (resolver.LockPolicy, []string, error) {
	if len(args) == 0 {
		return resolver.LockPolicy{IgnoreAll: true, DryRun: dryRun}, nil, nil
	}
	policy := resolver.LockPolicy{
		Float:   make(map[string]bool),
		Targets: make(map[string]string),
		DryRun:  dryRun,
	}
	requested := make([]string, 0, len(args))
	seen := make(map[string]bool, len(args))
	for _, arg := range args {
		pkgName, target, hasTarget, err := splitDepTargetArg(arg)
		if err != nil {
			return resolver.LockPolicy{}, nil, &usageError{err: err}
		}
		if err := validateUpdatablePkgRef(pkgName, links); err != nil {
			return resolver.LockPolicy{}, nil, err
		}
		if pinnedVer, ok := pins[pkgName]; ok && strings.TrimSpace(pinnedVer) != "" {
			return resolver.LockPolicy{}, nil, fmt.Errorf("package %s is pinned to %s in .vmake/project.json; use 'vmake deps pin %s@<version>' to change the pin or 'vmake deps unpin %s' first", pkgName, pinnedVer, pkgName, pkgName)
		}
		if !seen[pkgName] {
			seen[pkgName] = true
			requested = append(requested, pkgName)
		}
		if hasTarget {
			policy.Targets[pkgName] = target
			delete(policy.Float, pkgName)
		} else if _, hasExplicit := policy.Targets[pkgName]; !hasExplicit {
			policy.Float[pkgName] = true
		}
	}
	return policy, requested, nil
}

func splitDepTargetArg(arg string) (string, string, bool, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", "", false, fmt.Errorf("empty package argument")
	}
	if idx := strings.LastIndex(arg, "@"); idx >= 0 {
		pkgName := arg[:idx]
		target := arg[idx+1:]
		if pkgName == "" || target == "" {
			return "", "", false, fmt.Errorf("invalid package@version reference %q", arg)
		}
		return pkgName, target, true, nil
	}
	return arg, "", false, nil
}

func validateUpdatablePkgRef(pkgName string, links map[string]string) error {
	if path, ok := links[pkgName]; ok {
		return fmt.Errorf("package %s is linked to %s; run 'vmake pkg unlink %s' first", pkgName, path, pkgName)
	}
	return nil
}

func validateRequestedPackagesInGraph(ctx *RuntimeContext, requested []string) error {
	if len(requested) == 0 || ctx == nil || ctx.DepGraph == nil {
		return nil
	}
	subParents := ctx.Resolver.SubParents()
	for _, name := range requested {
		node := ctx.DepGraph.Packages[name]
		if node == nil {
			return fmt.Errorf("package %q is not in the project dependency graph", name)
		}
		if parent := subParents[name]; parent != "" {
			return fmt.Errorf("sub-package %s inherits its parent %s; update or pin %s instead", name, parent, parent)
		}
		if node.IsLocal() && (node.Pkg == nil || len(node.Pkg.GitURLs()) == 0) {
			return fmt.Errorf("local package %q has no SetGit source", name)
		}
	}
	return nil
}

func refreshRegistryReposForUpdate(requested []string) error {
	mgr := getRepoManager()
	filter := make(map[string]bool)
	for _, pkgName := range requested {
		if repoName, _, ok := strings.Cut(pkgName, "/"); ok && repoName != "" {
			filter[repoName] = true
		}
	}
	if len(requested) > 0 && len(filter) == 0 {
		return nil
	}
	for _, info := range mgr.ListInfo() {
		if info.Native {
			continue
		}
		if len(filter) > 0 && !filter[info.Name] {
			continue
		}
		lock, err := mgr.Lock(info.Name, true)
		if err != nil {
			return err
		}
		err = mgr.UpdateLocked(info.Name)
		lock.Release()
		if err != nil {
			return err
		}
	}
	return nil
}

func printDepsStatusTable(w io.Writer, report *pipeline.DepsStatusReport) error {
	if len(report.Packages) == 0 && len(report.StaleLockEntries) == 0 {
		fmt.Fprintln(w, "No remote or Git-managed dependencies in the active configuration.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PACKAGE\tSOURCE\tVERSION\tCOMMIT\tPINNED\tTREE\tSTATE")
	for _, e := range report.Packages {
		ver := "-"
		if e.Version != "" {
			ver = e.Version
		}
		commit := "-"
		if e.Commit != "" {
			commit = repo.ShortCommit(e.Commit)
		}
		pinned := "-"
		if e.Pinned != "" {
			pinned = e.Pinned
		}
		tree := "missing"
		if e.Linked != "" {
			tree = "->" + e.Linked
			if e.Dirty {
				tree += " (dirty)"
			}
		} else if e.Materialized {
			tree = "materialized"
			if e.Dirty {
				tree += " (dirty)"
			}
		} else if e.Cached {
			tree = "cached"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", e.Name, e.Source, ver, commit, pinned, tree, e.State)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(report.StaleLockEntries) > 0 {
		fmt.Fprintf(w, "\nStale lockfile entries (run 'vmake deps sync' to prune): %s\n", strings.Join(report.StaleLockEntries, ", "))
	}
	return nil
}

func printDepsUpdateSummary(w io.Writer, action string, res *pipeline.DepsUpdateResult, links map[string]string) {
	if len(res.Changes) == 0 {
		if res.DryRun {
			fmt.Fprintln(w, "No lockfile changes would be made.")
		} else {
			fmt.Fprintf(w, "%s dependencies (vmake.lock unchanged).\n", action)
		}
	} else {
		fmt.Fprintf(w, "%s .vmake/vmake.lock:\n", action)
		printDepsChanges(w, res.Changes)
	}
	if len(links) > 0 {
		fmt.Fprintln(w, "Linked packages keep their existing lock entries; run 'vmake pkg unlink <pkg>' before updating their version.")
	}
}

func printDepsChanges(w io.Writer, changes []lockfile.Change) {
	for _, ch := range changes {
		switch ch.Kind {
		case "added":
			ver := lockfile.DisplayVersion(ch.NewVersion, ch.NewCommit)
			fmt.Fprintf(w, "  + %s %s (%s)\n", ch.Name, ver, repo.ShortCommit(ch.NewCommit))
		case "updated":
			oldVer := lockfile.DisplayVersion(ch.OldVersion, ch.OldCommit)
			newVer := lockfile.DisplayVersion(ch.NewVersion, ch.NewCommit)
			fmt.Fprintf(w, "  ~ %s %s (%s) -> %s (%s)\n", ch.Name, oldVer, repo.ShortCommit(ch.OldCommit), newVer, repo.ShortCommit(ch.NewCommit))
		case "removed":
			oldVer := lockfile.DisplayVersion(ch.OldVersion, ch.OldCommit)
			fmt.Fprintf(w, "  - %s %s (%s)\n", ch.Name, oldVer, repo.ShortCommit(ch.OldCommit))
		}
	}
}

func writeDepsUpdateJSON(w io.Writer, command string, res *pipeline.DepsUpdateResult) error {
	pkgs := map[string]*lockfile.LockedPkg{}
	if res.Lock != nil && res.Lock.Packages != nil {
		pkgs = res.Lock.Packages
	}
	changes := res.Changes
	if changes == nil {
		changes = []lockfile.Change{}
	}
	return writeDepsJSON(w, &depsUpdateEnvelope{
		SchemaVersion: 1,
		Command:       command,
		DryRun:        res.DryRun,
		Changes:       changes,
		Packages:      pkgs,
	})
}

func writeDepsJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func completeDepsPkgRef(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	seen := make(map[string]bool)
	var refs []string
	if l, err := lockfile.Load(getLockfilePath()); err == nil {
		for _, name := range l.SortedNames() {
			seen[name] = true
			refs = append(refs, name)
		}
	}
	if pins, err := config.LoadProjectPins(findProjectDirSoft()); err == nil {
		for name := range pins {
			if !seen[name] {
				seen[name] = true
				refs = append(refs, name)
			}
		}
	}
	extra, _ := completePkgRef(cmd, args, toComplete)
	for _, name := range extra {
		if !seen[name] {
			seen[name] = true
			refs = append(refs, name)
		}
	}
	return refs, cobra.ShellCompDirectiveNoFileComp
}
