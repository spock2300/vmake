package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
)

var cleanAllFlag bool

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean build artifacts",
	Long: `Remove build directories for the current configuration.
Use --all to remove build directories for every configuration.`,
	Run: runClean,
}

func init() {
	RootCmd.AddCommand(cleanCmd)
	cleanCmd.Flags().BoolVar(&cleanAllFlag, "all", false, "clean build directories for every configuration")
}

type pkgCleanEntry struct {
	Dir, Name string
}

func cleanPackages(entries []pkgCleanEntry, ctx *RuntimeContext, cleanAll bool) error {
	if cleanAll {
		for _, pkg := range entries {
			cleanAllBuildDirs(pkg.Dir, pkg.Name)
		}
		vlog.Info("Clean completed!")
		return nil
	}

	insp, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{SkipUnresolvedPackages: true})
	if err != nil {
		return err
	}

	for _, pkg := range entries {
		if dirs := insp.PkgDirs[pkg.Name]; dirs != nil && dirs.BuildDir != "" {
			cleanDir(dirs.BuildDir, pkg.Name, filepath.Base(dirs.BuildDir))
		}
	}

	vlog.Info("Clean completed!")
	return nil
}

func scanPackages(workDir string) []pkgCleanEntry {
	packages, err := buildscript.Scan(workDir)
	fatalErr(err)

	entries := make([]pkgCleanEntry, len(packages))
	for i, pkg := range packages {
		entries[i] = pkgCleanEntry{Dir: pkg.Dir, Name: pkg.Name}
	}
	return entries
}

func runClean(cmd *cobra.Command, args []string) {
	ctx, ok := resolveToConfigBestEffort(false)
	if !ok {
		if !cleanAllFlag {
			fatalErr(fmt.Errorf("project configuration is unavailable; run 'vmake clean --all' or 'vmake distclean' to remove every build directory"))
		}
		entries := scanPackages(ctx.WorkDir)
		fatalErr(cleanPackages(entries, ctx, true))
		return
	}

	vlog.Info("")
	vlog.Info("Executing OnClean...")
	if err := executeCleanHooks(ctx, false, true); err != nil {
		if !cleanAllFlag {
			fatalErr(err)
		}
		vlog.Error("Skipping OnClean: %v", err)
	}

	entries := collectCleanEntries(ctx)
	fatalErr(cleanPackages(entries, ctx, cleanAllFlag))
}

func collectCleanEntries(ctx *RuntimeContext) []pkgCleanEntry {
	var entries []pkgCleanEntry
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.Source != nil && node.IsLocal() {
			entries = append(entries, pkgCleanEntry{Dir: node.Source.Dir, Name: name})
		}
	}
	return entries
}

func executeCleanHooks(ctx *RuntimeContext, localOnly, skipUnresolved bool) (err error) {
	defer scriptcall.Recover(&err)
	var insp *pipeline.Inspection
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.Pkg == nil || node.Source == nil {
			continue
		}
		if localOnly && !node.IsLocal() {
			continue
		}
		hasHooks := false
		node.Pkg.ExecCleanFuncs(node.Source.Dir, func(api.CleanFunc) { hasHooks = true })
		if !hasHooks {
			continue
		}
		if insp == nil {
			var err error
			insp, err = pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{SkipUnresolvedPackages: skipUnresolved})
			if err != nil {
				return fmt.Errorf("OnClean: %w", err)
			}
		}
		dirs := insp.PkgDirs[name]
		if dirs == nil || dirs.BuildDir == "" {
			continue
		}

		platform, err := pipeline.PackagePlatform(ctx, name)
		if err != nil {
			return fmt.Errorf("OnClean: %w", err)
		}
		values, err := pipeline.PackageConfigValues(ctx, name, insp.GlobalValues)
		if err != nil {
			return fmt.Errorf("OnClean: %w", err)
		}
		pipeline.DetectExistingSrcDir(node)

		node.Pkg.SetDirs(*dirs)
		node.Pkg.SetToolchain(insp.PackageToolchains[name])
		node.Pkg.SetPlatform(platform)

		cleanCtx := api.NewCleanContext(name, values)
		if opts, ok := ctx.AllOptions[name]; ok {
			cleanCtx.SetOptions(maps.Clone(opts))
		}
		cleanCtx.MergeGlobals(ctx.GlobalOptions, insp.GlobalValues)
		cleanCtx.SetPackage(node.Pkg)
		node.Pkg.SetOptions(cleanCtx.Options)
		node.Pkg.SetCfgVals(cleanCtx.CfgVals)

		node.Pkg.ExecCleanFuncs(dirs.SourceDir, func(fn api.CleanFunc) {
			fn(cleanCtx)
		})
	}
	return nil
}

func cleanDir(path, pkgName, label string) bool {
	if !fs.FileExists(path) {
		return false
	}
	if err := fs.RemoveAll(path); err != nil {
		vlog.Error("Failed to clean %s/%s: %v", pkgName, label, err)
		return false
	}
	vlog.Info("Cleaned %s/%s/", pkgName, label)
	return true
}

func removeIfExists(path, pkgName, label string, isDir bool) {
	if !fs.FileExists(path) {
		return
	}
	var err error
	if isDir {
		err = fs.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		if pkgName != "" {
			vlog.Error("Failed to clean %s/%s: %v", pkgName, label, err)
		} else {
			vlog.Error("Failed to clean %s: %v", label, err)
		}
		return
	}
	if pkgName != "" {
		vlog.Info("Cleaned %s/%s", pkgName, label)
	} else {
		vlog.Info("Cleaned %s", label)
	}
}

func cleanAllBuildDirs(dir, pkgName string) {
	buildBase := filepath.Join(dir, "build")
	entries, err := os.ReadDir(buildBase)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		cleanDir(filepath.Join(buildBase, name), pkgName, name)
	}
}
