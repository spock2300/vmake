package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/toolchain"
)

var cleanAllFlag bool

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean build artifacts",
	Long: `Remove object files and build cache for all packages.
Use --all to clean all build directories.`,
	Run: runClean,
}

func init() {
	RootCmd.AddCommand(cleanCmd)
	cleanCmd.Flags().BoolVar(&cleanAllFlag, "all", false, "clean all build directories")
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

	cfg := ctx.Config
	platform, err := pipeline.ProjectPlatform(ctx)
	if err != nil {
		return err
	}

	tcName := pipeline.ResolveToolchainName(cfg, "")
	resolvedTools, err := existingCleanTools(tcName, platform)
	if err != nil {
		return err
	}

	mode := pipeline.ResolveMode(cfg, "")

	tcNames := collectToolchainNames(cfg, tcName, entries)

	for _, pkg := range entries {
		entry := config.GetEntry(cfg, pkg.Name)
		cleaned := false
		for _, name := range tcNames {
			cc := resolvedTools.CC
			if name != tcName {
				tools, err := existingCleanTools(name, platform)
				if err != nil {
					continue
				}
				cc = tools.CC
			}
			if cleanBuildKeyDir(pkg.Dir, pkg.Name, name, cc, mode, entry.Options) {
				cleaned = true
			}
		}
		if !cleaned {
			cleanAllBuildDirs(pkg.Dir, pkg.Name)
		}
	}

	vlog.Info("Clean completed!")
	return nil
}

func existingCleanTools(name string, platform api.Platform) (*build.ResolvedTools, error) {
	tc, err := toolchain.GetManager().GetToolchain(name)
	if err == nil {
		if errs := toolchain.ValidateToolchain(tc); len(errs) > 0 {
			err = fmt.Errorf("invalid toolchain %q: %w", name, errors.Join(errs...))
		} else {
			var tools *build.ResolvedTools
			tools, err = build.ResolveTools(tc, platform)
			if err == nil {
				return tools, nil
			}
		}
	}
	return nil, fmt.Errorf("%w; run 'vmake build --toolchain %s' first", err, name)
}

func collectToolchainNames(cfg *config.ConfigFile, defaultTc string, entries []pkgCleanEntry) []string {
	seen := make(map[string]bool)
	seen[defaultTc] = true

	if cfg.Global != nil && cfg.Global.Toolchain != "" {
		seen[cfg.Global.Toolchain] = true
	}

	for _, entry := range cfg.Entries {
		if v, ok := entry.Options["toolchain"].(string); ok && v != "" {
			seen[v] = true
		}
	}

	for _, pkg := range entries {
		pkgCfgPath := filepath.Join(pkg.Dir, ".vmake", "config.json")
		pkgCfg, err := config.Load(pkgCfgPath)
		if err != nil {
			continue
		}
		if pkgCfg.Global != nil && pkgCfg.Global.Toolchain != "" {
			seen[pkgCfg.Global.Toolchain] = true
		}
		for _, entry := range pkgCfg.Entries {
			if v, ok := entry.Options["toolchain"].(string); ok && v != "" {
				seen[v] = true
			}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
		entries := scanPackages(ctx.WorkDir)
		fatalErr(cleanPackages(entries, ctx, cleanAllFlag))
		return
	}

	vlog.Info("")
	vlog.Info("Executing OnClean...")
	if err := executeCleanHooks(ctx, false); err != nil {
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

func executeCleanHooks(ctx *RuntimeContext, localOnly bool) error {
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
			insp, err = pipeline.Inspect(ctx)
			if err != nil {
				return fmt.Errorf("OnClean: %w", err)
			}
		}
		dirs := insp.PkgDirs[name]
		if dirs == nil {
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
		node.Pkg.SetToolchain(insp.Tc)
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

func cleanBuildKeyDir(dir, pkgName, tcName, ccPath, mode string, options map[string]any) bool {
	buildKey := build.BuildKey(ccPath, mode, options, build.GlobalFlagsHash())
	return cleanDir(build.BuildPath(dir, buildKey, ""), pkgName, buildKey)
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
