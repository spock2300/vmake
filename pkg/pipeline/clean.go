package pipeline

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	vlog "github.com/spock2300/vmake/pkg/log"
)

type PkgCleanEntry struct {
	Dir  string
	Name string
}

func RunClean(ctx *RuntimeContext, projectOpened, cleanAll bool) error {
	if !projectOpened {
		if !cleanAll {
			return fmt.Errorf("project configuration is unavailable; run 'vmake clean --all' or 'vmake distclean' to remove every build directory")
		}
		entries, err := ScanPackages(ctx.WorkDir)
		if err != nil {
			return err
		}
		if err := CleanPackages(entries, ctx, true); err != nil {
			return err
		}
		RemoveStaleCompileDatabase(ctx)
		return nil
	}

	vlog.Info("")
	vlog.Info("Executing OnClean...")
	if err := ExecuteCleanHooks(ctx, false, true, !cleanAll); err != nil {
		if !cleanAll {
			return err
		}
		vlog.Error("Skipping OnClean: %v", err)
	}

	entries := CollectCleanEntries(ctx)
	insp, err := CleanPackagesInspection(entries, ctx, cleanAll)
	if err != nil {
		return err
	}
	RemoveStaleCompileDatabase(ctx)
	if !cleanAll {
		if remaining := RemainingBuildDirectories(ctx, PackageDirsOf(insp)); remaining > 0 {
			vlog.Info("Note: %d build director(ies) for other configurations remain; use 'vmake clean --all' or 'vmake distclean' to remove them.", remaining)
		}
	}
	return nil
}

func CleanPackages(entries []PkgCleanEntry, ctx *RuntimeContext, cleanAll bool) error {
	_, err := CleanPackagesInspection(entries, ctx, cleanAll)
	return err
}

func CleanPackagesInspection(entries []PkgCleanEntry, ctx *RuntimeContext, cleanAll bool) (*Inspection, error) {
	if cleanAll {
		for _, pkg := range entries {
			CleanAllBuildDirs(pkg.Dir, pkg.Name)
		}
		CleanAllRemoteOutputs(ctx.Paths.DepsDir, ctx.SourceLinks)
		vlog.Info("Clean completed!")
		return nil, nil
	}

	insp, err := InspectWithOptions(ctx, InspectOptions{SkipUnresolvedPackages: true, FilterDeps: true, FreezeDeps: true})
	if err != nil {
		return nil, err
	}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil {
			continue
		}
		view, ok := insp.Package(name)
		if !ok || view.Skipped || (insp.Needed() != nil && !view.Needed) || view.Dirs.BuildDir == "" {
			continue
		}
		dirs := view.Dirs
		if node.IsLocal() {
			CleanDir(dirs.BuildDir, name, filepath.Base(dirs.BuildDir))
			continue
		}
		output := filepath.Dir(dirs.BuildDir)
		CleanDir(output, name, filepath.Base(output))
	}

	vlog.Info("Clean completed!")
	return insp, nil
}

func PackageDirsOf(insp *Inspection) func(name string) *api.PkgDirs {
	return func(name string) *api.PkgDirs {
		if insp == nil {
			return nil
		}
		if view, ok := insp.Package(name); ok && !view.Skipped {
			return view.Dirs
		}
		return nil
	}
}

func RemainingBuildDirectories(ctx *RuntimeContext, dirsFor func(name string) *api.PkgDirs) int {
	active := make(map[string]map[string]bool)
	mark := func(parent, key string) {
		if active[parent] == nil {
			active[parent] = make(map[string]bool)
		}
		if key != "" {
			active[parent][key] = true
		}
	}
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil {
			continue
		}
		var dirs *api.PkgDirs
		if dirsFor != nil {
			dirs = dirsFor(name)
		}
		if node.IsLocal() {
			key := ""
			if dirs != nil && dirs.BuildDir != "" {
				key = filepath.Base(dirs.BuildDir)
			}
			mark(filepath.Join(node.Source.Dir, "build"), key)
			continue
		}
		if dirs == nil || dirs.BuildDir == "" {
			continue
		}
		keyDir := filepath.Dir(dirs.BuildDir)
		mark(filepath.Dir(keyDir), filepath.Base(keyDir))
	}

	total := 0
	for parent, current := range active {
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && !current[entry.Name()] {
				total++
			}
		}
	}
	return total
}

func RemoveStaleCompileDatabase(ctx *RuntimeContext) {
	if ctx == nil || ctx.Paths == nil || ctx.Paths.ProjectDir == "" {
		return
	}
	RemoveIfExists(filepath.Join(ctx.Paths.ProjectDir, "build", "compile_commands.json"), "", "build/compile_commands.json", false)
}

func CleanAllRemoteOutputs(depDir string, links map[string]string) {
	_ = filepath.WalkDir(depDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "state.json" {
			return nil
		}
		root := filepath.Dir(path)
		CleanOutputDirs(filepath.Join(root, "out"), filepath.Base(root))
		return nil
	})
	for name := range links {
		CleanOutputDirs(filepath.Join(depDir, filepath.FromSlash(name), "out"), name)
	}
}

func CleanOutputDirs(outDir, pkgName string) {
	owners, err := os.ReadDir(outDir)
	if err != nil {
		return
	}
	for _, owner := range owners {
		if !owner.IsDir() || strings.HasPrefix(owner.Name(), ".") {
			continue
		}
		ownerDir := filepath.Join(outDir, owner.Name())
		builds, err := os.ReadDir(ownerDir)
		if err != nil {
			continue
		}
		for _, build := range builds {
			if !build.IsDir() || strings.HasPrefix(build.Name(), ".") {
				continue
			}
			CleanDir(filepath.Join(ownerDir, build.Name()), pkgName, build.Name())
		}
	}
}

func ScanPackages(workDir string) ([]PkgCleanEntry, error) {
	packages, err := buildscript.Scan(workDir)
	if err != nil {
		return nil, err
	}
	entries := make([]PkgCleanEntry, len(packages))
	for i, pkg := range packages {
		entries[i] = PkgCleanEntry{Dir: pkg.Dir, Name: pkg.Name}
	}
	return entries, nil
}

func CollectCleanEntries(ctx *RuntimeContext) []PkgCleanEntry {
	var entries []PkgCleanEntry
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.Source != nil && node.IsLocal() {
			entries = append(entries, PkgCleanEntry{Dir: node.Source.Dir, Name: name})
		}
	}
	return entries
}

func ExecuteCleanHooks(ctx *RuntimeContext, localOnly, skipUnresolved, filterDeps bool) (err error) {
	defer scriptcall.Recover(&err)
	var insp *Inspection
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.Pkg == nil || node.Source == nil {
			continue
		}
		if localOnly && !node.IsLocal() {
			continue
		}
		hasHooks := false
		api.BindPackageExecCleanFuncs(node.Pkg, node.Source.Dir, func(api.CleanFunc) { hasHooks = true })
		if !hasHooks {
			continue
		}
		if insp == nil {
			var err error
			insp, err = InspectWithOptions(ctx, InspectOptions{SkipUnresolvedPackages: skipUnresolved, FilterDeps: filterDeps, FreezeDeps: filterDeps})
			if err != nil {
				return fmt.Errorf("OnClean: %w", err)
			}
		}
		view, ok := insp.Package(name)
		if !ok || view.Skipped || (filterDeps && insp.Needed() != nil && !view.Needed) || view.Dirs.BuildDir == "" {
			continue
		}
		dirs := view.Dirs

		platform, err := PackagePlatform(ctx, name)
		if err != nil {
			return fmt.Errorf("OnClean: %w", err)
		}
		values, err := PackageConfigValues(ctx, name, insp.GlobalValues)
		if err != nil {
			return fmt.Errorf("OnClean: %w", err)
		}
		DetectExistingSrcDir(node)

		node.Pkg.SetDirs(*dirs)
		node.Pkg.SetToolchain(view.Toolchain)
		node.Pkg.SetPlatform(platform)

		pkgOpts := ctx.allOptions[name]
		cleanCtx := api.NewCleanContext(name, values)
		if pkgOpts != nil {
			cleanCtx.SetOptions(maps.Clone(pkgOpts))
		}
		cleanCtx.MergeGlobals(
			visibleGlobalOptionsFor(node, pkgOpts, ctx.globalOptions),
			visibleGlobalValuesFor(node, pkgOpts, ctx.globalOptions, insp.GlobalValues),
		)
		api.BindSetCleanPackage(cleanCtx, node.Pkg)
		node.Pkg.SetOptions(api.BindOptions(&cleanCtx.ConfigAccessor))
		node.Pkg.SetCfgVals(api.BindCfgVals(&cleanCtx.ConfigAccessor))

		api.BindPackageExecCleanFuncs(node.Pkg, dirs.SourceDir, func(fn api.CleanFunc) {
			fn(cleanCtx)
		})
	}
	return nil
}

func CleanDir(path, pkgName, label string) bool {
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

func RemoveIfExists(path, pkgName, label string, isDir bool) {
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

func CleanAllBuildDirs(dir, pkgName string) {
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

		CleanDir(filepath.Join(buildBase, name), pkgName, name)
	}
}
