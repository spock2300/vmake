package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
)

var distCleanPurgeCache bool

var distCleanCmd = &cobra.Command{
	Use:   "distclean",
	Short: "Deep clean all build artifacts",
	Long: `Remove all build artifacts including compiled buildscripts,
the install directory, and the vmake_deps/ symlink farm.

This is equivalent to 'vmake clean --all' plus:
  - build/compile_commands.json for each local package
  - install/ directory at project root
  - vmake_deps/ directory (per-project symlinks into the global cache)

The global cache (~/.vmake/cache) is shared across projects and survives
distclean by default: a rebuild re-links from it without recompiling.
Use --purge-cache to also delete cached sources and binary outputs for
every remote package this project has materialized. Warning: this affects
all other projects using those packages too.

Use 'vmake pkg clean <repo/name>' to purge individual packages.`,
	Run: runDistClean,
}

func init() {
	distCleanCmd.Flags().BoolVar(&distCleanPurgeCache, "purge-cache", false,
		"also remove global cache (sources and binary outputs) for this project's remote packages")
	RootCmd.AddCommand(distCleanCmd)
}

func runDistClean(cmd *cobra.Command, args []string) {
	ctx, ok := resolveToConfigBestEffort(false)
	if ok {
		vlog.Info("")
		vlog.Info("Executing OnClean...")
		if err := executeCleanHooks(ctx, false); err != nil {
			vlog.Error("Skipping OnClean: %v", err)
		}
	}

	entries := scanPackages(ctx.WorkDir)
	if err := cleanPackages(entries, ctx, true); err != nil {
		vlog.Error("Error: %v", err)
	}

	for _, pkg := range entries {
		removeIfExists(filepath.Join(pkg.Dir, "build", "compile_commands.json"), pkg.Name, "compile_commands.json", false)
	}

	removeIfExists(filepath.Join(ctx.WorkDir, "install"), "", "install/", true)

	if distCleanPurgeCache {
		purgeGlobalCache(getDepsDir())
	}

	removeIfExists(getDepsDir(), "", "vmake_deps/", true)

	vlog.Info("Distclean completed!")
}

// purgeGlobalCache removes the global cache entries for every remote package
// materialized under vmake_deps/. The symlink farm is the authoritative list
// of what this project downloaded; entries are removed through their real
// cache locations (never through the symlinks).
func purgeGlobalCache(depsDir string) {
	entries, err := os.ReadDir(depsDir)
	if err != nil {
		vlog.Info("No vmake_deps/ to purge")
		return
	}

	sourceMgr := repo.NewSourceManager(depsDir, getCacheDir())

	for _, repoEntry := range entries {
		if !repoEntry.IsDir() {
			continue
		}
		repoName := repoEntry.Name()
		pkgs, err := os.ReadDir(filepath.Join(depsDir, repoName))
		if err != nil {
			continue
		}
		for _, pkgEntry := range pkgs {
			if !pkgEntry.IsDir() {
				continue
			}
			pkgName := pkgEntry.Name()
			srcLink := filepath.Join(depsDir, repoName, pkgName, "src")
			target, err := os.Readlink(srcLink)
			if err != nil {
				vlog.Error("resolve src link %s: %v", srcLink, err)
				continue
			}
			version := filepath.Base(filepath.Dir(target))
			if err := sourceMgr.CleanVersion(repoName, pkgName, version); err != nil {
				vlog.Error("purge %s/%s@%s: %v", repoName, pkgName, version, err)
				continue
			}
			vlog.Info("purged cache for %s/%s@%s", repoName, pkgName, version)
		}
	}
}
