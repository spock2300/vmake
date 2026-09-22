package pipeline

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
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

func isLegacyRealSrcDir(node *resolver.PackageNode) bool {
	if !DetectExistingSrcDir(node) {
		return false
	}
	srcDir := filepath.Join(node.Source.Dir, "src")
	_, err := os.Readlink(srcDir)
	return err != nil
}

type Inspection struct {
	Mode            string
	TcName          string
	Tc              *toolchain.Toolchain
	GlobalValues    map[string]any
	Tools           *build.ResolvedTools
	Needed          map[string]bool
	GlobalFlagsHash string
	PkgDirs         map[string]*api.PkgDirs
}

func Inspect(ctx *RuntimeContext) (*Inspection, error) {
	pre, err := prepareBuildPrelude(ctx)
	if err != nil {
		return nil, err
	}

	pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)
	toolCache := map[api.Platform]*build.ResolvedTools{pre.cfg.Platform: pre.tools}

	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil {
			continue
		}
		tools, err := resolvePackageTools(ctx, name, pre.cfg.Tc, toolCache)
		if err != nil {
			return nil, err
		}
		entry := config.GetEntry(ctx.Config, name)
		scriptHash, err := scriptHashForNode(name, node)
		if err != nil {
			return nil, err
		}
		flagsHash := packageFlagsHash(pre.globalFlagsHash, node)
		if node.IsLocal() {
			pkgDirs[name] = makeLocalPkgDirs(node.Source.Dir, tools.CCKey(), pre.cfg.Mode, entry.Options, flagsHash, scriptHash)
			continue
		}
		sourceDir := filepath.Join(ctx.Paths.DepsDir, name, "src")
		if info, err := os.Stat(sourceDir); err != nil || !info.IsDir() {
			continue
		}
		version, commit, ok := remoteVersionKey(ctx, name, node, entry)
		if !ok {
			continue
		}
		repoName, pkgName, _ := api.SplitPackageRef(name)
		versionDir := filepath.Join(ctx.Paths.CacheDir, repoName, pkgName, version)
		patchHash, err := patchHashForNode(name, node)
		if err != nil {
			return nil, err
		}
		pkgDirs[name] = makeRemotePkgDirs(versionDir, sourceDir, tools.CCKey(), pre.cfg.Mode, entry.Options,
			version, commit, flagsHash, patchHash, scriptHash)
	}

	return &Inspection{
		Mode:            pre.cfg.Mode,
		TcName:          pre.cfg.TcName,
		Tc:              pre.cfg.Tc,
		GlobalValues:    pre.cfg.GlobalValues,
		Tools:           pre.tools,
		Needed:          pre.needed,
		GlobalFlagsHash: pre.globalFlagsHash,
		PkgDirs:         pkgDirs,
	}, nil
}
