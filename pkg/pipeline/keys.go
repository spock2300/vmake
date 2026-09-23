package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type buildConfig struct {
	Mode         string
	TcName       string
	Tc           *toolchain.Toolchain
	Platform     api.Platform
	GlobalValues map[string]any
}

type buildPrelude struct {
	cfg             *buildConfig
	tools           *build.ResolvedTools
	needed          map[string]bool
	globalFlagsHash string
}

func resolveExistingBuildConfig(ctx *RuntimeContext) (*buildConfig, error) {
	if _, err := ProjectPlatform(ctx); err != nil {
		return nil, err
	}
	tcName := ResolveToolchainName(ctx.Config, ctx.ToolchainOverride)
	tc, err := toolchain.GetManager().GetToolchain(tcName)
	if err != nil {
		return nil, fmt.Errorf("%w; run 'vmake build --toolchain %s' first", err, tcName)
	}
	if errs := toolchain.ValidateToolchain(tc); len(errs) > 0 {
		return nil, fmt.Errorf("invalid toolchain %q: %w; run 'vmake build --toolchain %s' first", tcName, errors.Join(errs...), tcName)
	}
	return makeBuildConfig(ctx, tc, tcName), nil
}

func prepareBuildPrelude(ctx *RuntimeContext) (*buildPrelude, error) {
	cfg, err := resolveExistingBuildConfig(ctx)
	if err != nil {
		return nil, err
	}
	tools, err := build.ResolveTools(cfg.Tc, cfg.Platform)
	if err != nil {
		return nil, err
	}
	needed, err := computeReachable(ctx.DepGraph)
	if err != nil {
		return nil, err
	}
	applyGlobalFlagsFromNeeded(ctx, needed)
	return &buildPrelude{
		cfg:             cfg,
		tools:           tools,
		needed:          needed,
		globalFlagsHash: build.GlobalFlagsHash(),
	}, nil
}

func resolvePackageTools(ctx *RuntimeContext, name string, tc *toolchain.Toolchain, cache map[string]*build.ResolvedTools) (*build.ResolvedTools, error) {
	platform, err := PackagePlatform(ctx, name)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s:%s:%s", tc.Name, platform.OS, platform.Triple)
	if tools := cache[key]; tools != nil {
		return tools, nil
	}
	tools, err := build.ResolveTools(tc, platform)
	if err != nil {
		return nil, fmt.Errorf("resolve tools for %s: %w", name, err)
	}
	cache[key] = tools
	return tools, nil
}

func scriptHashForNode(name string, node *resolver.PackageNode) (string, error) {
	if node == nil || node.Source == nil {
		return "", nil
	}
	h, err := buildscript.ScriptSetHash(node.Source.Dir)
	if err != nil {
		return "", fmt.Errorf("hash buildscript for %s: %w", name, err)
	}
	return h, nil
}

func patchHashForNode(name string, node *resolver.PackageNode) (string, error) {
	if node == nil || node.Pkg == nil || len(node.Pkg.GetPatches()) == 0 {
		return "", nil
	}
	h, err := repo.PatchSetHash(node.Pkg)
	if err != nil {
		return "", fmt.Errorf("hash patches for %s: %w", name, err)
	}
	return h, nil
}

func remoteVersionKey(ctx *RuntimeContext, name string, node *resolver.PackageNode, entry *config.EntryConfig) (string, string, bool) {
	if entry != nil && entry.Version != "" {
		commit := ""
		if ctx.Lock != nil && !ctx.IgnoreLock {
			if locked, ok := ctx.Lock.Get(name); ok && locked.Version == entry.Version {
				commit = locked.Commit
			}
		}
		return entry.Version, commit, true
	}
	if ctx.Lock != nil && !ctx.IgnoreLock {
		if locked, ok := ctx.Lock.Get(name); ok && locked.Version != "" {
			return locked.Version, locked.Commit, true
		}
	}
	if node.Native != nil && node.Native.Selected != "" {
		return node.Native.Selected, node.Native.Commit, true
	}
	return "", "", false
}

func computeReachable(graph *resolver.Graph) (map[string]bool, error) {
	needed := make(map[string]bool, len(graph.Packages))
	var queue []string

	var rootID string
	rootCount := 0
	for id, node := range graph.Packages {
		if node.IsLocal() && node.Pkg != nil && node.Pkg.IsRoot() {
			rootCount++
			rootID = id
		}
	}
	if rootCount > 1 {
		return nil, fmt.Errorf("SetRoot(true): multiple root packages found; only one is allowed")
	}

	if rootCount == 1 {
		needed[rootID] = true
		queue = append(queue, rootID)
	} else {
		if os.Getenv("VMAKE_LEGACY_ROOT") != "1" {
			rootsHint := make([]string, 0)
			for id, node := range graph.Packages {
				if node.IsLocal() && node.Pkg != nil && len(node.Pkg.GetRequireFuncs()) > 0 {
					rootsHint = append(rootsHint, id)
				}
			}
			vlog.Info("[hint] no package declares SetRoot(true); consider adding 'p.SetRoot(true)' to one of: %s", strings.Join(rootsHint, ", "))
			vlog.Info("[hint] set VMAKE_LEGACY_ROOT=1 to silence this warning (legacy heuristic will be used)")
		}
		dependedOn := make(map[string]bool)
		for _, node := range graph.Packages {
			if !node.IsLocal() {
				continue
			}
			for _, dep := range node.Deps {
				if depNode, ok := graph.Packages[dep]; ok && depNode.IsLocal() {
					dependedOn[dep] = true
				}
			}
		}

		hasLocalRequire := false
		for _, node := range graph.Packages {
			if node.IsLocal() && node.Pkg != nil && len(node.Pkg.GetRequireFuncs()) > 0 {
				hasLocalRequire = true
				break
			}
		}

		for id, node := range graph.Packages {
			if !node.IsLocal() {
				continue
			}
			if hasLocalRequire && node.Pkg != nil && len(node.Pkg.GetRequireFuncs()) == 0 && dependedOn[id] {
				continue
			}
			needed[id] = true
			queue = append(queue, id)
		}
	}

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if node, ok := graph.Packages[name]; ok {
			for _, dep := range node.Deps {
				if !needed[dep] {
					needed[dep] = true
					queue = append(queue, dep)
				}
			}
		}
	}
	return needed, nil
}

func collectAllPkgOptions(ctx *RuntimeContext, needed map[string]bool) map[string]map[string]any {
	result := make(map[string]map[string]any)
	for _, name := range ctx.Resolver.GetOrder() {
		if !needed[name] {
			continue
		}
		entry := config.GetEntry(ctx.Config, name)
		opts := make(map[string]any, len(entry.Options))
		for k, v := range entry.Options {
			opts[k] = v
		}
		result[name] = opts
	}
	return result
}

func packageFlagsHash(globalFlagsHash string, node *resolver.PackageNode) string {
	if node == nil || node.Pkg == nil || !node.Pkg.DefaultVisibilityHidden() {
		return globalFlagsHash
	}
	hash := sha256.Sum256([]byte(globalFlagsHash + "\x00default-visibility=hidden"))
	return hex.EncodeToString(hash[:])[:16]
}

func localKeyExtra(globalFlagsHash, scriptHash string, sourceCommit ...string) string {
	commit := ""
	if len(sourceCommit) > 0 {
		commit = sourceCommit[0]
	}
	return build.JoinKeyExtra("", commit, globalFlagsHash, "", scriptHash)
}

func makeLocalPkgDirs(scriptDir, ccKey, mode string, opts map[string]any, globalFlagsHash, scriptHash string, sourceCommit ...string) *api.PkgDirs {
	buildKey := build.BuildKey(ccKey, mode, opts, localKeyExtra(globalFlagsHash, scriptHash, sourceCommit...))
	return &api.PkgDirs{
		SourceDir: scriptDir,
		BuildDir:  filepath.Join(scriptDir, "build", buildKey),
	}
}

func makeRemotePkgDirs(versionDir, sourceDir, ccKey, mode string, opts map[string]any, version, commit, globalFlagsHash, patchHash, scriptHash string, memberPath ...string) *api.PkgDirs {
	buildKey := build.BuildKey(ccKey, mode, opts, build.JoinKeyExtra(version, commit, globalFlagsHash, patchHash, scriptHash))
	member := ""
	if len(memberPath) > 0 {
		member = memberPath[0]
	}
	output := filepath.Join(versionDir, "out", storage.OwnerKey(filepath.ToSlash(member)), buildKey)
	return &api.PkgDirs{
		SourceDir:  filepath.Join(output, "work", "repo", member),
		BuildDir:   filepath.Join(output, "build"),
		InstallDir: filepath.Join(output, "install"),
	}
}

func remoteMemberPath(ctx *RuntimeContext, name string) string {
	if parent, ok := ctx.Resolver.SubParents()[name]; ok {
		return strings.TrimPrefix(name, parent+"/")
	}
	return ""
}

func remoteOwnerName(ctx *RuntimeContext, name string) string {
	if parent, ok := ctx.Resolver.SubParents()[name]; ok {
		return parent
	}
	return name
}

func applyPatches(pkg *api.Package, sourceDir string) error {
	return applyPatchesContext(context.Background(), pkg, sourceDir)
}

func applyPatchesContext(ctx context.Context, pkg *api.Package, sourceDir string) error {
	patches := pkg.GetPatches()
	if len(patches) == 0 {
		return nil
	}

	scriptDir := pkg.ScriptDir()
	vlog.Info("Applying patches for %s", pkg.FullName())

	for _, patch := range patches {
		absPath := filepath.Join(scriptDir, patch)
		applied, err := repo.IsPatchAppliedContext(ctx, sourceDir, absPath)
		if err != nil {
			return err
		}
		if applied {
			vlog.Info("  %s (already applied)", patch)
			continue
		}
		vlog.Info("  %s", patch)
		if err := repo.ApplyPatchContext(ctx, sourceDir, absPath); err != nil {
			return err
		}
	}

	return nil
}

func rebaseKConfigSourceDirs(ctx *RuntimeContext, name, oldRoot, newRoot string) error {
	if oldRoot == newRoot {
		return nil
	}
	member, seedRoot := "", oldRoot
	if ctx.DepGraph != nil {
		if node := ctx.DepGraph.Packages[name]; node != nil && !node.IsLocal() && node.Source != nil {
			member = remoteMemberPath(ctx, name)
			seedRoot = node.Source.Dir
		}
	}
	for _, k := range ctx.AllKConfigs[name] {
		if srcDir := k.SrcDir(); srcDir != "" {
			rebased, err := rebaseSourcePath(srcDir, oldRoot, newRoot, member, seedRoot)
			if err != nil {
				return fmt.Errorf("rebase kconfig %s: %w", name, err)
			}
			k.SetSrcDir(rebased)
		}
	}
	return nil
}

func restoreKConfigFiles(ctx *RuntimeContext, pkgDirs map[string]*api.PkgDirs, needed map[string]bool) error {
	for _, name := range ctx.Resolver.GetOrder() {
		if !needed[name] {
			continue
		}
		kconfigs := ctx.AllKConfigs[name]
		if len(kconfigs) == 0 {
			continue
		}

		entry := config.GetEntry(ctx.Config, name)
		kconfigContent := entry.KConfig
		hasEntry := ctx.Config.Entries != nil && ctx.Config.Entries[name] != nil

		k := kconfigs[0]
		srcDir := k.SrcDir()
		if srcDir == "" {
			srcDir = pkgDirs[name].SourceDir
		} else if ctx.DepGraph != nil {
			if node := ctx.DepGraph.Packages[name]; node != nil && !node.IsLocal() && node.Source != nil {
				if err := rebaseKConfigSourceDirs(ctx, name, node.Source.Dir, pkgDirs[name].SourceDir); err != nil {
					return err
				}
				srcDir = k.SrcDir()
			}
		}
		configPath := filepath.Join(srcDir, k.ConfigPath())

		if hasEntry && kconfigContent == "" {
			fs.RemoveIfExists(configPath)
			continue
		}

		if kconfigContent == "" {
			continue
		}

		existing, err := os.ReadFile(configPath)
		if err == nil && string(existing) == kconfigContent {
			continue
		}

		if err := fs.EnsureParentDir(configPath); err != nil {
			return fmt.Errorf("restore kconfig %s: %w", name, err)
		}
		if err := os.WriteFile(configPath, []byte(kconfigContent), 0644); err != nil {
			return fmt.Errorf("restore kconfig %s: %w", name, err)
		}
		if err := api.ApplyKConfigPatches(configPath, k.Patches()); err != nil {
			return fmt.Errorf("restore kconfig %s: %w", name, err)
		}
		vlog.Info("Restored .config for %s (%d bytes)", name, len(kconfigContent))
	}
	return nil
}
