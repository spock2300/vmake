package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
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
	PackageToolchains  map[string]*toolchain.Toolchain
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
	if opts.FilterDeps {
		if err := filterDependencies(ctx, false); err != nil {
			return nil, err
		}
		if opts.FreezeDeps {
			ctx.DepGraph.Freeze()
		}
	}
	pre, err := prepareBuildPrelude(ctx, opts.FilterDeps)
	if err != nil {
		return nil, err
	}
	if opts.FilterDeps {
		if err := validateNeededSubPackageOwners(pre.needed, ctx.Resolver.SubParents()); err != nil {
			return nil, err
		}
	}
	globalCFlags, globalCxxFlags, globalLdFlags, globalLinks := neededGlobalFlags(ctx, pre.needed)

	pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)
	toolCache := make(map[string]*build.ResolvedTools)
	packageToolchains := make(map[string]*toolchain.Toolchain)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil {
			continue
		}
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
		entry := config.GetEntry(ctx.Config, name)
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
				Options:   entry.Options,
				Material:  keyMaterial{commit: commit, flagsHash: flagsHash},
				Ready:     true,
			})
			if err != nil {
				return nil, err
			}
			pkgDirs[name] = &layout.Dirs
			continue
		}
		owner := remoteOwnerName(ctx, name)
		member := remoteMemberPath(ctx, name)
		tree := remoteTreeDir(ctx, owner)
		sourceDir := filepath.Join(tree, "src", filepath.FromSlash(member))
		if info, err := os.Stat(sourceDir); err != nil || !info.IsDir() {
			continue
		}
		version, commit, ok := remoteVersionKey(ctx, owner, ctx.DepGraph.Packages[owner], config.GetEntry(ctx.Config, owner))
		if !ok {
			continue
		}
		patchHash, err := patchHashForNode(name, node)
		if err != nil {
			return nil, err
		}
		if commit != "" && member != "" && node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
			sourceMgr := repo.NewSourceManager(ctx.Paths.DepsDir, ctx.Paths.CacheDir).WithSession(ctx.Locks).WithContext(ctx.Context)
			if sourceCommit := sourceMgr.CommitAt(sourceDir); sourceCommit != "" {
				commit = sourceCommitKey(commit, sourceCommit)
			}
		}
		if commit == "" {
			continue
		}
		layout, err := layoutFor(layoutInput{
			PkgRoot:  tree,
			Member:   member,
			CCKey:    tools.CCKey(),
			Mode:     pre.cfg.Mode,
			Options:  entry.Options,
			Material: keyMaterial{version: version, commit: commit, flagsHash: flagsHash, patchHash: patchHash},
			Ready:    true,
		})
		if err != nil {
			return nil, err
		}
		pkgDirs[name] = &layout.Dirs
		if member != "" && node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
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
		PackageToolchains:  packageToolchains,
	}, nil
}

func neededGlobalFlags(ctx *RuntimeContext, needed map[string]bool) (cflags, cxxflags, ldflags, links []string) {
	for _, name := range ctx.Resolver.GetOrder() {
		if !needed[name] {
			continue
		}
		buf := ctx.BufferedGlobalFlags[name]
		if buf == nil {
			continue
		}
		cflags = append(cflags, buf.cFlags...)
		cxxflags = append(cxxflags, buf.cxxFlags...)
		ldflags = append(ldflags, buf.ldFlags...)
		links = append(links, buf.links...)
	}
	return cflags, cxxflags, ldflags, links
}
