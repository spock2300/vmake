package main

import (
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
	"github.com/spock2300/vmake/pkg/resolver"
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

func cleanPackages(entries []pkgCleanEntry, cfg *config.ConfigFile, cleanAll bool) {
	tc, tcName, err := GetToolchain(cfg)
	if err != nil {
		vlog.Error("Error: %v", err)
		return
	}

	resolvedTools, err := build.ResolveTools(tc)
	if err != nil {
		vlog.Error("Error: %v", err)
		return
	}

	mode := resolveMode(cfg, "")

	tcNames := collectToolchainNames(cfg, tcName, entries)

	for _, pkg := range entries {
		if cleanAll {
			cleanAllBuildDirs(pkg.Dir, pkg.Name)
		} else {
			entry := config.GetEntry(cfg, pkg.Name)
			cleaned := false
			for _, name := range tcNames {
				t := tc
				cc := resolvedTools.CC
				if name != tcName {
					t, err = toolchain.GetManager().SelectToolchain(name)
					if err != nil {
						continue
					}
					tools, err := build.ResolveTools(t)
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
	}

	vlog.Info("Clean completed!")
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
	ctx, ok := resolveToConfigBestEffort()
	if !ok {
		entries := scanPackages(ctx.WorkDir)
		cleanPackages(entries, ctx.Config, cleanAllFlag)
		return
	}

	vlog.Info("")
	vlog.Info("Executing OnClean...")
	executeCleanHooks(ctx, false)

	entries := collectCleanEntries(ctx)
	cleanPackages(entries, ctx.Config, cleanAllFlag)
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

func executeCleanHooks(ctx *RuntimeContext, localOnly bool) {
	cfg, err := resolveBuildConfig(ctx)
	if err != nil {
		vlog.Error("Error: %v", err)
		return
	}

	resolvedTools, err := build.ResolveTools(cfg.Tc)
	if err != nil {
		vlog.Error("Error: %v", err)
		return
	}

	pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)
	localPkgOptions := collectLocalPkgOptions(ctx)
	depsDir := getDepsDir()

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.Pkg == nil || node.Source == nil {
			continue
		}
		if localOnly && !node.IsLocal() {
			continue
		}

		entry := config.GetEntry(ctx.Config, name)
		if node.IsLocal() {
			pkgDirs[name] = resolvePkgDir(node, depsDir, resolvedTools.CC, cfg.Mode, localPkgOptions[name])
		} else {
			sourceDir := filepath.Join(depsDir, name, "src")
			if info, err := os.Stat(sourceDir); err != nil || !info.IsDir() {
				continue
			}
			pkgDirs[name] = resolvePkgDir(node, depsDir, resolvedTools.CC, cfg.Mode, entry.Options)
		}

		detectExistingSrcDir(node)

		node.Pkg.SetDirs(*pkgDirs[name])
		node.Pkg.SetToolchain(cfg.Tc)

		cleanCtx := api.NewCleanContext(name, entry.Options)
		if opts, ok := ctx.AllOptions[name]; ok {
			cleanCtx.SetOptions(opts)
		}
		cleanCtx.MergeGlobals(ctx.GlobalOptions, cfg.GlobalValues)
		cleanCtx.SetPackage(node.Pkg)

		node.Pkg.ExecCleanFuncs(pkgDirs[name].SourceDir, func(fn api.CleanFunc) {
			fn(cleanCtx)
		})
	}
}

func detectExistingSrcDir(node *resolver.PackageNode) bool {
	if !node.IsLocal() || node.Pkg == nil {
		return false
	}
	if len(node.Pkg.GitURLs()) == 0 {
		return false
	}
	srcDir := filepath.Join(node.Source.Dir, "src")
	if info, err := os.Stat(srcDir); err == nil && info.IsDir() {
		if _, err := os.Stat(filepath.Join(srcDir, ".git")); err == nil {
			node.Pkg.SetSrcDir(srcDir)
			return true
		}
	}
	return false
}

func cleanBuildKeyDir(dir, pkgName, tcName, ccPath, mode string, options map[string]any) bool {
	buildKey := build.BuildKey(ccPath, mode, options)
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
