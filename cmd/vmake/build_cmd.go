package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the project",
	Long:  `Compile and link all targets defined in build.go files.`,
	RunE:  runBuild,
}

func init() {
	RootCmd.AddCommand(buildCmd)
	addInstallFlags(buildCmd)
	addBuildFlags(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	commandStorageLocks()
	execution := &RuntimeContext{}
	return withBuildContext(execution, func() error {
		if manifestFlag != "" {
			if err := importManifestIntoLock(execution.Context, manifestFlag); err != nil {
				return err
			}
		}
		ctx, err := resolveToConfigContext(execution.Context, false)
		if err != nil {
			return err
		}
		if manifestFlag != "" {
			if err := checkoutManifestLocals(ctx, manifestFlag); err != nil {
				return err
			}
		}
		result, err := runBuildPhase(ctx, BuildOptions{IncludeTests: testsFlag, Jobs: jobsFlag, KeepGoing: keepGoingFlag})
		if err != nil {
			return err
		}
		if installFlag {
			return executeInstall(ctx, result)
		}
		return nil
	})
}

// importManifestIntoLock pins remote package versions from an install manifest
// into vmake.lock BEFORE dependency resolution, so the graph is built from the
// pinned versions (not from latest-matching tags).
func importManifestIntoLock(ctx context.Context, manifestPath string) error {
	var mf installManifest
	if err := jsonio.Load(manifestPath, &mf); err != nil {
		return err
	}

	lockPath := getLockfilePath()
	l, err := lockfile.LoadOrCreate(lockPath)
	if err != nil {
		return err
	}
	changed := false
	for _, entry := range mf.Packages {
		switch entry.Source {
		case "native", "registry":
			if entry.Ref == "" {
				vlog.Info("  skip manifest entry %s (empty ref)", entry.Name)
				continue
			}
			version := entry.Version
			if version == "" {
				version = entry.Ref
			}
			if entry.URL == "" {
				return fmt.Errorf("manifest entry %s has no URL; cannot resolve commit for %s", entry.Name, entry.Ref)
			}
			commit, err := repo.ResolveRemoteCommitContext(ctx, entry.URL, entry.Ref)
			if err != nil {
				return fmt.Errorf("resolve commit for %s@%s: %w", entry.Name, entry.Ref, err)
			}
			l.Set(entry.Name, &lockfile.LockedPkg{
				Version: version,
				Commit:  commit,
				Source:  entry.Source,
			})
			vlog.Info("  pin %s -> %s (%s, commit %s)", entry.Name, version, entry.Source, shortCommit(commit))
			changed = true
		}
	}
	if changed {
		return l.Save(lockPath)
	}
	return nil
}

// checkoutManifestLocals checks out local packages to the refs recorded in an
// install manifest (local packages are not part of vmake.lock). Local packages
// with a managed git source are skipped: their manifest ref belongs to the
// upstream clone, while the checkout would run against the project repository,
// and the build materializes the recorded source version on its own.
func checkoutManifestLocals(ctx *RuntimeContext, manifestPath string) error {
	var mf installManifest
	if err := jsonio.Load(manifestPath, &mf); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	for _, entry := range mf.Packages {
		if entry.Source != "local" {
			continue
		}
		if entry.Ref == "" || entry.Ref == "unknown" || entry.Path == "" {
			continue
		}
		if node := localManifestNode(ctx, entry.Name); node != nil && len(node.Pkg.GitURLs()) > 0 {
			vlog.Info("  skip checkout %s (managed git source)", entry.Name)
			continue
		}
		if err := repo.CheckoutContext(ctx.Context, filepath.Join(cwd, entry.Path), entry.Ref); err != nil {
			return err
		}
		shortRef := entry.Ref
		if len(shortRef) > 12 {
			shortRef = shortRef[:12]
		}
		vlog.Info("  checkout %s -> %s", entry.Name, shortRef+"...")
	}
	return nil
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
