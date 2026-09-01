package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the project",
	Long:  `Compile and link all targets defined in build.go files.`,
	Run:   runBuild,
}

func init() {
	RootCmd.AddCommand(buildCmd)
	addInstallFlags(buildCmd)
	addBuildFlags(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) {
	if manifestFlag != "" {
		importManifestIntoLock(manifestFlag)
	}
	ctx := resolveToConfig(false)
	if manifestFlag != "" {
		checkoutManifestLocals(manifestFlag)
	}
	result, err := runBuildPhase(ctx, BuildOptions{IncludeTests: testsFlag, Jobs: jobsFlag, KeepGoing: keepGoingFlag})
	fatalErr(err)
	if installFlag {
		fatalErr(executeInstall(ctx, result))
	}
}

// importManifestIntoLock pins remote package versions from an install manifest
// into vmake.lock BEFORE dependency resolution, so the graph is built from the
// pinned versions (not from latest-matching tags).
func importManifestIntoLock(manifestPath string) {
	var mf installManifest
	fatalErr(jsonio.Load(manifestPath, &mf))

	lockPath := getLockfilePath()
	l := mustLoadLockfile(lockPath)
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
				fatalMsg("manifest entry %s has no URL; cannot resolve commit for %s", entry.Name, entry.Ref)
			}
			commit, err := repo.ResolveRemoteCommit(entry.URL, entry.Ref)
			if err != nil {
				fatalErr(fmt.Errorf("resolve commit for %s@%s: %w", entry.Name, entry.Ref, err))
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
		fatalErr(l.Save(lockPath))
	}
}

// checkoutManifestLocals checks out local packages to the refs recorded in an
// install manifest (local packages are not part of vmake.lock).
func checkoutManifestLocals(manifestPath string) {
	var mf installManifest
	fatalErr(jsonio.Load(manifestPath, &mf))

	cwd, err := os.Getwd()
	fatalErr(err)

	for _, entry := range mf.Packages {
		if entry.Source != "local" {
			continue
		}
		if entry.Ref == "" || entry.Ref == "unknown" {
			continue
		}
		fatalErr(repo.Checkout(filepath.Join(cwd, entry.Path), entry.Ref))
		shortRef := entry.Ref
		if len(shortRef) > 12 {
			shortRef = shortRef[:12]
		}
		vlog.Info("  checkout %s -> %s", entry.Name, shortRef+"...")
	}
}
