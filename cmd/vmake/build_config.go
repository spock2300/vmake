package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

func computeReachable(graph *resolver.Graph) map[string]bool {
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
		vlog.Fatal("SetRoot(true): multiple root packages found; only one is allowed")
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
	return needed
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

func collectLocalPkgOptions(ctx *RuntimeContext) map[string]map[string]any {
	result := make(map[string]map[string]any)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node.IsLocal() {
			entry := config.GetEntry(ctx.Config, name)
			opts := make(map[string]any, len(entry.Options))
			for k, v := range entry.Options {
				opts[k] = v
			}
			result[name] = opts
		}
	}
	return result
}

func makeLocalPkgDirs(scriptDir, ccPath, mode string, opts map[string]any) *api.PkgDirs {
	buildKey := build.BuildKey(ccPath, mode, opts)
	return &api.PkgDirs{
		SourceDir: scriptDir,
		BuildDir:  filepath.Join(scriptDir, "build", buildKey),
	}
}

func makeRemotePkgDirs(depsDir, name, ccPath, mode string, opts map[string]any, sourceDir string) *api.PkgDirs {
	buildKey := build.BuildKey(ccPath, mode, opts)
	return &api.PkgDirs{
		SourceDir:  sourceDir,
		BuildDir:   filepath.Join(depsDir, name, "out", buildKey, "build"),
		InstallDir: filepath.Join(depsDir, name, "out", buildKey, "install"),
	}
}

func resolvePkgDir(node *resolver.PackageNode, depsDir, ccPath, mode string, opts map[string]any) *api.PkgDirs {
	if node.IsLocal() {
		return makeLocalPkgDirs(node.Source.Dir, ccPath, mode, opts)
	}
	sourceDir := filepath.Join(depsDir, node.ID, "src")
	return makeRemotePkgDirs(depsDir, node.ID, ccPath, mode, opts, sourceDir)
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
		api.ApplyKConfigPatches(configPath, k.Patches())
		vlog.Info("Restored .config for %s (%d bytes)", name, len(kconfigContent))
	}
	return nil
}

type BuildGoInfo struct {
	Versions map[string]string
	GitURLs  []string
}

func ParseBuildGo(path string) (*BuildGoInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)
	info := &BuildGoInfo{
		Versions: make(map[string]string),
	}
	for _, call := range []struct {
		name    string
		handler func([]string)
	}{
		{"AddVersion", func(args []string) {
			if len(args) >= 2 {
				info.Versions[args[0]] = args[1]
			}
		}},
		{"SetGit", func(args []string) {
			if len(args) > 0 {
				info.GitURLs = append(info.GitURLs, args[0])
			}
		}},
	} {
		prefix := call.name + "("
		for i := 0; i+len(prefix) <= len(content); i++ {
			if content[i:i+len(prefix)] == prefix {
				call.handler(extractCallArgs(content[i+len(prefix):]))
			}
		}
	}
	return info, nil
}

func extractCallArgs(s string) []string {
	var args []string
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			i++
			start := i
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' {
					i++
				}
				i++
			}
			args = append(args, s[start:i])
			if len(args) >= 3 {
				break
			}
		} else if s[i] == ')' {
			break
		}
	}
	return args
}
