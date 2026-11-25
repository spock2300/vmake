package main

import (
	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/pipeline"
)

var cleanAllFlag bool

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean build artifacts",
	Long: `Remove build outputs for the current configuration.
Use --all to remove outputs for every configuration. Downloaded source trees
under .vmake_deps/ are kept; use 'vmake distclean --purge-sources' to remove
them too.`,
	Run: runClean,
}

type pkgCleanEntry = pipeline.PkgCleanEntry

func cleanPackages(entries []pkgCleanEntry, ctx *RuntimeContext, cleanAll bool) error {
	return pipeline.CleanPackages(entries, ctx, cleanAll)
}

func remainingBuildDirectories(ctx *RuntimeContext, dirsFor func(name string) *api.PkgDirs) int {
	return pipeline.RemainingBuildDirectories(ctx, dirsFor)
}

func cleanAllRemoteOutputs(depDir string, links map[string]string) {
	pipeline.CleanAllRemoteOutputs(depDir, links)
}

func scanPackages(workDir string) []pkgCleanEntry {
	entries, err := pipeline.ScanPackages(workDir)
	fatalErr(err)
	return entries
}

func runClean(cmd *cobra.Command, args []string) {
	commandStorageLocks()
	ctx, ok := openProjectBestEffort(projectOptions{softGitignore: true})
	if ok {
		defer closeProject(ctx)
	}
	if ok || cleanAllFlag {
		fatalErr(invalidateBuildReport())
	}
	fatalErr(pipeline.RunClean(ctx, ok, cleanAllFlag))
}

func collectCleanEntries(ctx *RuntimeContext) []pkgCleanEntry {
	return pipeline.CollectCleanEntries(ctx)
}

func executeCleanHooks(ctx *RuntimeContext, localOnly, skipUnresolved, filterDeps bool) error {
	return pipeline.ExecuteCleanHooks(ctx, localOnly, skipUnresolved, filterDeps)
}

func removeIfExists(path, pkgName, label string, isDir bool) {
	pipeline.RemoveIfExists(path, pkgName, label, isDir)
}
