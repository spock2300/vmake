package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
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
	GlobalValues map[string]any
}

type buildPrelude struct {
	cfg             *buildConfig
	tools           *build.ResolvedTools
	needed          map[string]bool
	globalFlagsHash string
}

func prepareBuildPrelude(ctx *RuntimeContext) (*buildPrelude, error) {
	tcName := ResolveToolchainName(ctx.Config, ctx.ToolchainOverride)
	tc, err := toolchain.GetManager().GetToolchain(tcName)
	if err != nil {
		return nil, err
	}
	if errs := toolchain.ValidateToolchain(tc); len(errs) > 0 {
		return nil, fmt.Errorf("invalid toolchain %q: %w", tcName, errors.Join(errs...))
	}
	cfg := makeBuildConfig(ctx, tc, tcName)
	tools, err := build.ResolveTools(cfg.Tc)
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

func localKeyExtra(globalFlagsHash, scriptHash string) string {
	return build.JoinKeyExtra("", "", globalFlagsHash, "", scriptHash)
}

func makeLocalPkgDirs(scriptDir, ccKey, mode string, opts map[string]any, globalFlagsHash, scriptHash string) *api.PkgDirs {
	buildKey := build.BuildKey(ccKey, mode, opts, localKeyExtra(globalFlagsHash, scriptHash))
	return &api.PkgDirs{
		SourceDir: scriptDir,
		BuildDir:  filepath.Join(scriptDir, "build", buildKey),
	}
}

func makeRemotePkgDirs(versionDir, sourceDir, ccKey, mode string, opts map[string]any, version, commit, globalFlagsHash, patchHash, scriptHash string) *api.PkgDirs {
	buildKey := build.BuildKey(ccKey, mode, opts, build.JoinKeyExtra(version, commit, globalFlagsHash, patchHash, scriptHash))
	return &api.PkgDirs{
		SourceDir:  sourceDir,
		BuildDir:   filepath.Join(versionDir, "out", buildKey, "build"),
		InstallDir: filepath.Join(versionDir, "out", buildKey, "install"),
	}
}

func applyPatches(pkg *api.Package, sourceDir string) error {
	patches := pkg.GetPatches()
	if len(patches) == 0 {
		return nil
	}

	scriptDir := pkg.ScriptDir()
	vlog.Info("Applying patches for %s", pkg.FullName())

	for _, patch := range patches {
		absPath := filepath.Join(scriptDir, patch)
		if repo.IsPatchApplied(sourceDir, absPath) {
			vlog.Info("  %s (already applied)", patch)
			continue
		}
		vlog.Info("  %s", patch)
		if err := repo.ApplyPatch(sourceDir, absPath); err != nil {
			return err
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
