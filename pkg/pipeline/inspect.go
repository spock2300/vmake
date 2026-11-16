package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
	needed             NeededSet
	GlobalFlagsHash    string
	pkgDirs            map[string]*api.PkgDirs
	layouts            map[string]*build.PackageLayout
	packageToolchains  map[string]*toolchain.Toolchain
	packageValues      map[string]map[string]any
	packagePlatforms   map[string]api.Platform

	names   []string
	nameSet map[string]bool
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

	pkgDirs := packageSourceDirs(ctx)
	names := make([]string, 0, len(ctx.DepGraph.Packages))
	nameSet := make(map[string]bool, len(ctx.DepGraph.Packages))
	layouts := make(map[string]*build.PackageLayout)
	toolCache := make(map[string]*build.ResolvedTools)
	packageToolchains := make(map[string]*toolchain.Toolchain)
	packageValues := make(map[string]map[string]any)
	packagePlatforms := make(map[string]api.Platform)
	var filterNeeded NeededSet
	if opts.FilterDeps {
		filterNeeded = pre.needed
	}
	allPkgOptions := collectAllPkgOptions(ctx, filterNeeded)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil {
			continue
		}
		names = append(names, name)
		nameSet[name] = true
		if len(ctx.allOptions[name]) > 0 {
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
		// markNotReady records the package's layout as "exists but is not
		// materialized yet", which is the only state that can express an
		// unknown build key without inventing one.
		markNotReady := func() {
			sourceDir := ""
			if dirs := pkgDirs[name]; dirs != nil {
				sourceDir = dirs.SourceDir
			}
			layout := build.PackageLayout{Dirs: api.PkgDirs{SourceDir: sourceDir}, Platform: packagePlatforms[name], Ready: false}
			pkgDirs[name] = &layout.Dirs
			layouts[name] = &layout
		}
		if node.IsLocal() {
			if node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 && !DetectExistingSrcDir(node) {
				markNotReady()
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
				Facts:     LayoutFacts{Commit: commit, FlagsHash: flagsHash},
				Platform:  packagePlatforms[name],
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
			markNotReady()
			continue
		}
		version, commit, ok := remoteVersionKey(ctx, loc.Owner, ctx.DepGraph.Packages[loc.Owner])
		if !ok {
			markNotReady()
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
			markNotReady()
			continue
		}
		layout, err := layoutFor(layoutInput{
			PkgRoot:        loc.OwnerTreeRoot,
			SourceOverride: sourceLink,
			Member:         loc.Member,
			CCKey:          tools.CCKey(),
			Mode:           pre.cfg.Mode,
			Options:        pkgOptions,
			Facts:          LayoutFacts{Version: version, Commit: commit, FlagsHash: flagsHash, PatchHash: patchHash, SourceLink: sourceLink},
			Platform:       packagePlatforms[name],
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
		GlobalMacroDefines: append([]string(nil), ctx.globalMacroDefines...),
		Tools:              pre.tools,
		needed:             pre.needed,
		GlobalFlagsHash:    pre.globalFlagsHash,
		pkgDirs:            pkgDirs,
		layouts:            layouts,
		packageToolchains:  packageToolchains,
		packageValues:      packageValues,
		packagePlatforms:   packagePlatforms,
		names:              names,
		nameSet:            nameSet,
	}, nil
}

// PackageView is the per-package projection of an Inspection: everything a
// consumer needs about one package, joined once instead of per caller.
type PackageView struct {
	Name      string
	Dirs      *api.PkgDirs
	Layout    *build.PackageLayout
	Toolchain *toolchain.Toolchain
	Values    map[string]any
	Platform  *api.Platform
	Needed    bool
	Skipped   bool
}

// Packages returns the inspected package names in resolver order. An
// Inspection built by hand (tests, external callers) has no recorded order, so
// its package names are derived from the maps and sorted.
func (in *Inspection) Packages() []string {
	if in == nil {
		return nil
	}
	if in.names != nil {
		return append([]string(nil), in.names...)
	}
	seen := make(map[string]bool)
	for _, name := range in.derivedNames() {
		seen[name] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (in *Inspection) derivedNames() []string {
	if in == nil {
		return nil
	}
	names := make([]string, 0, len(in.pkgDirs)+len(in.needed))
	for name := range in.pkgDirs {
		names = append(names, name)
	}
	for name := range in.layouts {
		names = append(names, name)
	}
	for name := range in.packageToolchains {
		names = append(names, name)
	}
	for name := range in.packageValues {
		names = append(names, name)
	}
	for name := range in.packagePlatforms {
		names = append(names, name)
	}
	for name := range in.needed {
		names = append(names, name)
	}
	return names
}

// knowsPackage reports whether the inspection has a record for name. A
// hand-built Inspection carries no index, so membership is derived from its
// maps instead.
func (in *Inspection) knowsPackage(name string) bool {
	if in.nameSet != nil {
		return in.nameSet[name]
	}
	for _, candidate := range in.derivedNames() {
		if candidate == name {
			return true
		}
	}
	return false
}

// Needed returns the inspected package set. A nil set means the inspection ran
// without a filter and every resolved package is reported.
func (in *Inspection) Needed() NeededSet {
	if in == nil {
		return nil
	}
	return in.needed
}

// PkgInfo projects the view into the build package's value: the layout
// conversion when the inspection resolved one, else a directory-only entry.
func (v PackageView) PkgInfo() *build.PkgInfo {
	if v.Layout != nil {
		return v.Layout.PkgInfo()
	}
	dirs := api.PkgDirs{}
	if v.Dirs != nil {
		dirs = *v.Dirs
	}
	return build.NewPkgInfo(dirs, "")
}

// Package returns the view for a package the inspection knows about. ok is
// false for an unknown name; view.Skipped reports that the package was skipped
// (it has no directories), which keeps "skipped" distinct from "absent".
func (in *Inspection) Package(name string) (PackageView, bool) {
	if in == nil || !in.knowsPackage(name) {
		return PackageView{}, false
	}
	dirs := in.pkgDirs[name]
	view := PackageView{
		Name:      name,
		Dirs:      dirs,
		Layout:    in.layouts[name],
		Toolchain: in.packageToolchains[name],
		Values:    in.packageValues[name],
		Needed:    in.needed[name],
		Skipped:   dirs == nil,
	}
	if platform, ok := in.packagePlatforms[name]; ok {
		view.Platform = &platform
	}
	return view, true
}
