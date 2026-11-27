package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
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

func containerVersionKey(ctx *RuntimeContext, containerID string, c *resolver.ContainerNode) (string, string, bool) {
	if c != nil && c.IsLinked() {
		return resolver.LocalLinkVersion, c.Link.Commit, true
	}
	if c != nil && c.IsLocalPath() {
		return c.Version, c.Commit, true
	}
	nativeSelected, nativeCommit := "", ""
	if c != nil {
		nativeSelected, nativeCommit = c.Version, c.Commit
	}
	decision := ctx.lockQuery().Containers().Decide(containerID, nativeSelected, nativeCommit)
	switch decision.Provenance {
	case resolver.ProvenanceTargetCommit:
		return "", decision.Commit, true
	case resolver.ProvenanceTargetVersion, resolver.ProvenanceConfigPin:
		commit := decision.Commit
		if commit == "" && c != nil && c.Version == decision.Version {
			commit = c.Commit
		}
		return decision.Version, commit, true
	case resolver.ProvenanceLock, resolver.ProvenanceNative:
		return decision.Version, decision.Commit, true
	}
	return "", "", false
}

func sourceVersionKey(ctx *RuntimeContext, name string, node *resolver.PackageNode) (string, string, bool) {
	if node != nil && node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource {
		return resolver.LocalLinkVersion, node.Source.Link.Commit, true
	}
	decision := ctx.lockQuery().Sources().Decide(name, "", "")
	switch decision.Provenance {
	case resolver.ProvenanceTargetCommit:
		return "", decision.Commit, true
	case resolver.ProvenanceTargetVersion, resolver.ProvenanceConfigPin, resolver.ProvenanceLock:
		return decision.Version, decision.Commit, true
	}
	return "", "", false
}

func configRootOverrides(ctx *RuntimeContext) []string {
	if ctx == nil || ctx.Config == nil {
		return nil
	}
	return ctx.Config.RootOverrides()
}

func computeReachable(graph *resolver.Graph, rootOverrides ...string) (map[string]bool, error) {
	var clean []string
	for _, r := range rootOverrides {
		if t := strings.TrimSpace(r); t != "" {
			clean = append(clean, t)
		}
	}
	needed, err := computeNeededGraph(graph, clean)
	if err != nil {
		return nil, err
	}
	return needed.Set, nil
}

func computeRootSeeds(graph *resolver.Graph, rootOverrides []string) ([]string, error) {
	if len(rootOverrides) > 0 {
		roots := make([]string, 0, len(rootOverrides))
		for _, rootOverride := range rootOverrides {
			node, ok := graph.Packages[rootOverride]
			if !ok || node == nil {
				return nil, fmt.Errorf("config root %q not found in workspace", rootOverride)
			}
			if !node.IsLocal() {
				return nil, fmt.Errorf("config root %q must be a local workspace package", rootOverride)
			}
			roots = append(roots, rootOverride)
		}
		return roots, nil
	}

	if len(graph.Roots) > 0 {
		roots := make([]string, 0, len(graph.Roots))
		for _, root := range graph.Roots {
			if node := graph.Packages[root]; node != nil && node.IsLocal() {
				roots = append(roots, root)
			}
		}
		if len(roots) > 0 {
			return roots, nil
		}
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

	var roots []string
	for id, node := range graph.Packages {
		if !node.IsLocal() || dependedOn[id] {
			continue
		}
		roots = append(roots, id)
	}
	if len(roots) == 0 {
		for id, node := range graph.Packages {
			if node.IsLocal() {
				roots = append(roots, id)
			}
		}
	}
	sort.Strings(roots)
	return roots, nil
}

func collectAllPkgOptions(ctx *RuntimeContext, needed NeededSet) map[string]map[string]any {
	globalVals := config.BuildGlobalValues(ctx.Config)
	result := make(map[string]map[string]any)
	for _, name := range ctx.Resolver.GetOrder() {
		if needed != nil && !needed.Has(name) {
			continue
		}
		entry := config.GetEntry(ctx.Config, name)
		opts := make(map[string]any, len(entry.Options))
		for k, v := range entry.Options {
			opts[k] = v
		}
		if ctx.derivedOptions != nil {
			for optName, val := range ctx.derivedOptions[name] {
				if _, exists := opts[optName]; exists {
					continue
				}
				if opt := ctx.allOptions[name][optName]; opt != nil && api.ValuesEqual(val, opt.Default()) {
					continue
				}
				opts[optName] = val
			}
		}
		var node *resolver.PackageNode
		if ctx.DepGraph != nil {
			node = ctx.DepGraph.Packages[name]
		}
		for optName, opt := range visibleGlobalOptionsFor(node, ctx.allOptions[name], ctx.globalOptions) {
			if opt == nil || opt.ExportDefine() {
				continue
			}
			if localOpt := ctx.allOptions[name][optName]; localOpt != nil && !localOpt.IsGlobal() {
				continue
			}
			if _, exists := opts[optName]; exists {
				continue
			}
			if val, ok := globalVals[optName]; ok && val != nil {
				opts[optName] = val
			} else if opt.Default() != nil {
				opts[optName] = opt.Default()
			}
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

// LayoutFacts is the key material for a package layout. The build stage derives
// it from live source state and the inspection from the lock; for the same
// package both adapters must produce this same value.
type LayoutFacts struct {
	Version    string
	Commit     string
	PatchHash  string
	SourceLink string
	FlagsHash  string
}

func (m LayoutFacts) localExtra() string {
	// Buildscript text is intentionally not part of the build-directory key:
	// a comment-only build.go edit must not discard every action record. The
	// commands, includes, defines and file lists produced by the script are
	// captured by each action signature instead.
	extra := build.JoinKeyExtra("", m.Commit, m.FlagsHash, "")
	if m.SourceLink != "" {
		extra += m.SourceLink + "\x00"
	}
	return extra
}

func (m LayoutFacts) remoteExtra() string {
	// Patch and source identity affect the materialized tree, so they stay in
	// the remote build-directory key. Buildscript text is action-level.
	extra := build.JoinKeyExtra(m.Version, m.Commit, m.FlagsHash, m.PatchHash)
	if m.SourceLink != "" {
		extra += m.SourceLink + "\x00"
	}
	return extra
}

type pkgKey struct {
	ccKey   string
	mode    string
	options map[string]any
	extra   string
}

func newLocalPkgKey(ccKey, mode string, opts map[string]any, material LayoutFacts) pkgKey {
	return pkgKey{
		ccKey:   ccKey,
		mode:    mode,
		options: opts,
		extra:   material.localExtra(),
	}
}

func newRemotePkgKey(ccKey, mode string, opts map[string]any, material LayoutFacts) pkgKey {
	return pkgKey{
		ccKey:   ccKey,
		mode:    mode,
		options: opts,
		extra:   material.remoteExtra(),
	}
}

func (k pkgKey) buildKeyE() (string, error) {
	return build.BuildKeyE(k.ccKey, k.mode, k.options, k.extra)
}

func remoteOwnerName(ctx *RuntimeContext, name string) string {
	if ctx != nil && ctx.DepGraph != nil {
		if node := ctx.DepGraph.Packages[name]; node != nil && node.IsNative() && node.Container != "" {
			return node.Container
		}
	}
	return name
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
	member, seedRoot := "", oldRoot
	if ctx.DepGraph != nil {
		if node := ctx.DepGraph.Packages[name]; node != nil && !node.IsLocal() && node.Source != nil {
			member = node.RelPath
			seedRoot = node.Source.Dir
		}
	}
	for _, k := range ctx.allKConfigs[name] {
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

func restoreKConfigFiles(ctx *RuntimeContext, pkgDirs map[string]*api.PkgDirs, needed NeededSet) error {
	for _, name := range ctx.Resolver.GetOrder() {
		if !needed[name] {
			continue
		}
		kconfigs := ctx.allKConfigs[name]
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
