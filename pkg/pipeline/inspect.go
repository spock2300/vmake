package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	Mode              string
	TcName            string
	Tc                *toolchain.Toolchain
	GlobalValues      map[string]any
	Tools             *build.ResolvedTools
	Needed            map[string]bool
	GlobalFlagsHash   string
	PkgDirs           map[string]*api.PkgDirs
	PackageToolchains map[string]*toolchain.Toolchain
}

type InspectOptions struct {
	SkipUnresolvedPackages bool
}

func Inspect(ctx *RuntimeContext) (*Inspection, error) {
	return InspectWithOptions(ctx, InspectOptions{})
}

func InspectWithOptions(ctx *RuntimeContext, opts InspectOptions) (*Inspection, error) {
	pre, err := prepareBuildPrelude(ctx)
	if err != nil {
		return nil, err
	}

	pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)
	toolCache := make(map[string]*build.ResolvedTools)
	packageToolchains := make(map[string]*toolchain.Toolchain)

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil {
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
		scriptHash, err := scriptHashForNode(name, node)
		if err != nil {
			return nil, err
		}
		flagsHash := packageFlagsHash(pre.globalFlagsHash, node)
		if node.IsLocal() {
			commit, err := existingLocalSourceCommit(node)
			if err != nil {
				return nil, err
			}
			pkgDirs[name] = makeLocalPkgDirs(node.Source.Dir, tools.CCKey(), pre.cfg.Mode, entry.Options, flagsHash, scriptHash, commit)
			continue
		}
		owner := remoteOwnerName(ctx, name)
		member := remoteMemberPath(ctx, name)
		sourceDir := filepath.Join(ctx.Paths.DepsDir, filepath.FromSlash(owner), "src", filepath.FromSlash(member))
		if info, err := os.Stat(sourceDir); err != nil || !info.IsDir() {
			continue
		}
		version, commit, ok := remoteVersionKey(ctx, owner, ctx.DepGraph.Packages[owner], config.GetEntry(ctx.Config, owner))
		if !ok {
			continue
		}
		versionDir := remoteVersionDir(ctx, owner, version)
		patchHash, err := patchHashForNode(name, node)
		if err != nil {
			return nil, err
		}
		if commit != "" && member != "" && node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
			materialized := nativeMemberSourceLink(ctx, name)
			if _, err := os.Stat(materialized); err == nil {
				sourceCommit, err := repo.GetCurrentCommitContext(ctx.Context, materialized)
				if err != nil {
					return nil, err
				}
				commit = sourceCommitKey(commit, sourceCommit)
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
		if commit == "" {
			continue
		}
		pkgDirs[name] = makeRemotePkgDirs(versionDir, sourceDir, tools.CCKey(), pre.cfg.Mode, entry.Options,
			version, commit, flagsHash, patchHash, scriptHash, member)
		if member != "" && node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
			node.Pkg.SetSrcDir(filepath.Join(pkgDirs[name].SourceDir, "src"))
		}
	}

	return &Inspection{
		Mode:              pre.cfg.Mode,
		TcName:            pre.cfg.TcName,
		Tc:                pre.cfg.Tc,
		GlobalValues:      pre.cfg.GlobalValues,
		Tools:             pre.tools,
		Needed:            pre.needed,
		GlobalFlagsHash:   pre.globalFlagsHash,
		PkgDirs:           pkgDirs,
		PackageToolchains: packageToolchains,
	}, nil
}
