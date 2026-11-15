package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func DetectExistingSrcDir(node *resolver.PackageNode) bool {
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

func hasUnmanagedSourceDir(node *resolver.PackageNode) bool {
	info, err := os.Lstat(filepath.Join(node.Source.Dir, "src"))
	return err == nil && info.IsDir()
}

type Inspection struct {
	Mode               string
	TcName             string
	Tc                 *toolchain.Toolchain
	GlobalValues       map[string]any
	GlobalCFlags       []string
	GlobalCxxFlags     []string
	GlobalLdFlags      []string
	GlobalLinks        []string
	GlobalMacroDefines []string
	Tools              *build.ResolvedTools
	Needed             map[string]bool
	GlobalFlagsHash    string
	PkgDirs            map[string]*api.PkgDirs
	Layouts            map[string]*build.PackageLayout
	PackageToolchains  map[string]*toolchain.Toolchain
	PackageValues      map[string]map[string]any
	PackagePlatforms   map[string]api.Platform
}

func ExpectedPackageSourceDir(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	if ctx == nil {
		return ""
	}
	return packageSourceTree(ctx, name, node)
}

func IsPackageSourcePresent(ctx *RuntimeContext, name string, node *resolver.PackageNode) bool {
	dir := ExpectedPackageSourceDir(ctx, name, node)
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

type InspectOptions struct {
	SkipUnresolvedPackages bool
	FilterDeps             bool
	// FreezeDeps seals the filtered graph for the rest of this invocation.
	// Commands that inspect before cleaning/building should set it so repeated
	// inspection does not execute OnRequire again or observe a different set.
	FreezeDeps bool
}

func Inspect(ctx *RuntimeContext) (*Inspection, error) {
	return InspectWithOptions(ctx, InspectOptions{})
}

func InspectWithOptions(ctx *RuntimeContext, opts InspectOptions) (_ *Inspection, err error) {
	defer scriptcall.Recover(&err)
	pre, err := prepareBuild(ctx, preludeOptions{
		filterDeps:        opts.FilterDeps,
		freezeDeps:        opts.FreezeDeps,
		resolveChip:       true,
		resolveConfig:     true,
		validateToolchain: true,
		resolveTools:      true,
	})
	if err != nil {
		return nil, err
	}
	if opts.FilterDeps {
		if err := validateNeededSubPackageOwners(pre.needed, ctx.Resolver.SubParents()); err != nil {
			return nil, err
		}
	}
	globalCFlags := pre.rawGlobalFlags[0]
	globalCxxFlags := pre.rawGlobalFlags[1]
	globalLdFlags := pre.rawGlobalFlags[2]
	globalLinks := pre.rawGlobalFlags[3]

	pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)
	layouts := make(map[string]*build.PackageLayout)
	toolCache := make(map[string]*build.ResolvedTools)
	packageToolchains := make(map[string]*toolchain.Toolchain)
	packageValues := make(map[string]map[string]any)
	packagePlatforms := make(map[string]api.Platform)
	var filterNeeded map[string]bool
	if opts.FilterDeps {
		filterNeeded = pre.needed
	}
	allPkgOptions := collectAllPkgOptions(ctx, filterNeeded)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil {
			continue
		}
		if len(ctx.AllOptions[name]) > 0 {
			packageValues[name] = DisplayPackageValues(ctx, name, pre.cfg.GlobalValues)
		}
		packagePlatforms[name] = platformFromValues(packagePlatformValues(ctx, name, pre.cfg.GlobalValues))
		if opts.FilterDeps && !pre.needed[name] {
			delete(pkgDirs, name)
			continue
		}
		tcName := resolvePkgToolchain(ctx.Config, name, pre.cfg.TcName)
		skippable := opts.SkipUnresolvedPackages && tcName != pre.cfg.TcName
		tc, err := toolchain.GetManager().GetToolchain(tcName)
		if err != nil {
			if skippable {
				delete(pkgDirs, name)
				vlog.Info("  %s: %v; skipping package", name, err)
				continue
			}
			return nil, fmt.Errorf("package %s: %w; run 'vmake build --toolchain %s' first", name, err, tcName)
		}
		if errs := toolchain.ValidateToolchain(tc); len(errs) > 0 {
			if skippable {
				delete(pkgDirs, name)
				vlog.Info("  %s: invalid toolchain %q: %v; skipping package", name, tcName, errors.Join(errs...))
				continue
			}
			return nil, fmt.Errorf("package %s has invalid toolchain %q: %w; run 'vmake build --toolchain %s' first", name, tcName, errors.Join(errs...), tcName)
		}
		packageToolchains[name] = tc
		tools, err := resolvePackageTools(ctx, name, tc, toolCache)
		if err != nil {
			if skippable {
				delete(pkgDirs, name)
				vlog.Info("  %s: %v; skipping package", name, err)
				continue
			}
			return nil, err
		}
		pkgOptions := allPkgOptions[name]
		flagsHash := packageFlagsHash(pre.globalFlagsHash, node)
		if node.IsLocal() {
			if node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 && !DetectExistingSrcDir(node) {
				continue
			}
			commit, err := existingLocalSourceCommit(node)
			if err != nil {
				return nil, err
			}
			layout, err := layoutFor(layoutInput{
				Local:     true,
				SourceDir: node.Source.Dir,
				CCKey:     tools.CCKey(),
				Mode:      pre.cfg.Mode,
				Options:   pkgOptions,
				Material:  keyMaterial{commit: commit, flagsHash: flagsHash},
				Ready:     true,
			})
			if err != nil {
				return nil, err
			}
			pkgDirs[name] = &layout.Dirs
			layouts[name] = &layout
			continue
		}
		loc := ctx.packageLocation(name, node)
		if info, err := os.Stat(loc.PackageRoot); err != nil || !info.IsDir() {
			continue
		}
		version, commit, ok := remoteVersionKey(ctx, loc.Owner, ctx.DepGraph.Packages[loc.Owner])
		if !ok {
			continue
		}
		patchHash, err := patchHashForNode(name, node)
		if err != nil {
			return nil, err
		}
		sourceLink := ""
		if loc.Linked {
			sourceLink = loc.PackageRoot
		}
		commitRoot := loc.PackageRoot
		if loc.ThirdPartyRoot != "" {
			commitRoot = loc.ThirdPartyRoot
		}
		if commit != "" && loc.Member != "" && node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
			sourceMgr := repo.NewSourceManager(ctx.Paths.DepsDir, ctx.Paths.CacheDir).WithSession(ctx.Locks).WithContext(ctx.Context)
			if sourceCommit := sourceMgr.CommitAt(commitRoot); sourceCommit != "" {
				commit = sourceCommitKey(commit, sourceCommit)
			}
		}
		if commit == "" {
			continue
		}
		layout, err := layoutFor(layoutInput{
			PkgRoot:        loc.OwnerTreeRoot,
			SourceOverride: sourceLink,
			Member:         loc.Member,
			CCKey:          tools.CCKey(),
			Mode:           pre.cfg.Mode,
			Options:        pkgOptions,
			Material:       keyMaterial{version: version, commit: commit, flagsHash: flagsHash, patchHash: patchHash, sourceLink: sourceLink},
			Ready:          true,
		})
		if err != nil {
			return nil, err
		}
		pkgDirs[name] = &layout.Dirs
		layouts[name] = &layout
		if loc.ThirdPartyRoot != "" {
			node.Pkg.SetSrcDir(loc.SourceTree)
		} else if loc.Member != "" && node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
			node.Pkg.SetSrcDir(filepath.Join(layout.Dirs.SourceDir, "src"))
		}
	}

	return &Inspection{
		Mode:               pre.cfg.Mode,
		TcName:             pre.cfg.TcName,
		Tc:                 pre.cfg.Tc,
		GlobalValues:       pre.cfg.GlobalValues,
		GlobalCFlags:       globalCFlags,
		GlobalCxxFlags:     globalCxxFlags,
		GlobalLdFlags:      globalLdFlags,
		GlobalLinks:        globalLinks,
		GlobalMacroDefines: append([]string(nil), ctx.GlobalMacroDefines...),
		Tools:              pre.tools,
		Needed:             pre.needed,
		GlobalFlagsHash:    pre.globalFlagsHash,
		PkgDirs:            pkgDirs,
		Layouts:            layouts,
		PackageToolchains:  packageToolchains,
		PackageValues:      packageValues,
		PackagePlatforms:   packagePlatforms,
	}, nil
}
