package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	exec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitcmd"
	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/version"
)

type installManifestEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  string `json:"source"`
	URL     string `json:"url,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Path    string `json:"path,omitempty"`
}

const installOwnershipMarker = ".vmake-install"

type installManifest struct {
	VMake     string                 `json:"vmake"`
	Toolchain string                 `json:"toolchain"`
	Mode      string                 `json:"mode"`
	Generated string                 `json:"generated"`
	Packages  []installManifestEntry `json:"packages"`
}

func executeInstall(ctx *RuntimeContext, result *BuildResult) (err error) {
	defer scriptcall.Recover(&err)
	if ctx.Context != nil && ctx.Context.Err() != nil {
		return ctx.Context.Err()
	}
	globalValues := result.GlobalValues

	effectivePrefix := prefixFlag
	if effectivePrefix == "" {
		effectivePrefix = filepath.Join(ctx.Paths.ProjectDir, "install")
	}

	vlog.Info("")
	vlog.Info("Installing...")

	if err := validateInstallPrefix(effectivePrefix, ctx.Paths.ProjectDir, installStorageRoots(ctx)...); err != nil {
		return err
	}
	if err := fs.RemoveAll(effectivePrefix); err != nil {
		return fmt.Errorf("clean install prefix: %w", err)
	}
	if err := fs.EnsureDir(effectivePrefix); err != nil {
		return fmt.Errorf("create install prefix: %w", err)
	}
	projectDir := ""
	if ctx.Paths != nil {
		projectDir = ctx.Paths.ProjectDir
	}
	defaultPrefix := ""
	if projectDir != "" {
		defaultPrefix = canonicalInstallPath(filepath.Join(projectDir, "install"))
	}
	targetPrefix := ""
	if absolute, err := filepath.Abs(effectivePrefix); err == nil {
		targetPrefix = canonicalInstallPath(absolute)
	} else {
		targetPrefix = canonicalInstallPath(effectivePrefix)
	}
	if defaultPrefix == "" || (targetPrefix != defaultPrefix && !installPathAncestor(defaultPrefix, targetPrefix)) {
		if err := os.WriteFile(filepath.Join(effectivePrefix, installOwnershipMarker), []byte("vmake\n"), 0644); err != nil {
			return fmt.Errorf("write install prefix marker: %w", err)
		}
	}

	installer := build.NewArtifactInstaller(result.Graph, result.PkgDirs, effectivePrefix)
	installer.SetInstallType(installTypeFlag)

	for _, name := range ctx.DepGraph.Order {
		if ctx.Context != nil && ctx.Context.Err() != nil {
			return ctx.Context.Err()
		}
		node := ctx.DepGraph.Packages[name]
		if node.Pkg == nil || result.BuildCtxs[name] == nil || result.SubGraphRoots[name] {
			continue
		}
		if err := installOnePackage(ctx, name, node, result, installer, globalValues); err != nil {
			return err
		}
	}

	if ctx.Context != nil && ctx.Context.Err() != nil {
		return ctx.Context.Err()
	}
	if err := installer.InstallAll(ctx.Context); err != nil {
		return err
	}
	result.InstallFiles = installer.InstalledFiles()

	if err := writeManifest(ctx, result, effectivePrefix); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	vlog.Info("")
	vlog.Info("Install succeeded!")
	return nil
}

func validateInstallPrefix(prefix, projectDir string, storageRoots ...string) error {
	if strings.TrimSpace(prefix) == "" {
		return fmt.Errorf("install prefix is empty")
	}
	absolute, err := filepath.Abs(prefix)
	if err != nil {
		return fmt.Errorf("resolve install prefix %q: %w", prefix, err)
	}
	target := canonicalInstallPath(absolute)
	if filepath.Dir(target) == target {
		return fmt.Errorf("refusing to clean filesystem root %q as install prefix", target)
	}
	if info, err := os.Stat(absolute); err == nil && !info.IsDir() {
		return fmt.Errorf("refusing to clean install prefix %q: it is not a directory", target)
	}
	protected := make([]string, 0, 4)
	if cwd, err := os.Getwd(); err == nil {
		protected = append(protected, cwd)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		protected = append(protected, home)
	}
	if projectDir != "" {
		protected = append(protected, projectDir)
	}
	projectRoot := ""
	if projectDir != "" {
		absolute, err := filepath.Abs(projectDir)
		if err == nil {
			projectRoot = canonicalInstallPath(absolute)
		}
	}
	for _, path := range protected {
		absolute, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		path = canonicalInstallPath(absolute)
		if target == path || installPathAncestor(target, path) {
			return fmt.Errorf("refusing to clean %q as install prefix: it is %q or one of its ancestors", target, path)
		}
	}
	for _, path := range storageRoots {
		if strings.TrimSpace(path) == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		path = canonicalInstallPath(absolute)
		if target == path || installPathAncestor(path, target) || installPathAncestor(target, path) {
			return fmt.Errorf("refusing to clean %q as install prefix: it overlaps vmake storage %q", target, path)
		}
	}
	defaultInstall := ""
	if projectRoot != "" {
		defaultInstall = canonicalInstallPath(filepath.Join(projectRoot, "install"))
		if target != defaultInstall && !installPathAncestor(defaultInstall, target) && installPathAncestor(projectRoot, target) {
			return fmt.Errorf("refusing to clean %q as install prefix: it is inside project root %q; use the default install directory or a path outside the project", target, projectRoot)
		}
	}
	if target != defaultInstall && !installPathAncestor(defaultInstall, target) {
		if info, err := os.Stat(target); err == nil && info.IsDir() && !installPrefixEmpty(target) && !fs.FileExists(filepath.Join(target, installOwnershipMarker)) {
			return fmt.Errorf("refusing to clean non-empty custom install prefix %q: use a new/empty directory or remove it manually; vmake only reuses directories carrying %s", target, installOwnershipMarker)
		}
	}
	return nil
}

func installPrefixEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

func installStorageRoots(ctx *RuntimeContext) []string {
	roots := []string{vmakeDir, getCacheDir(), getExtensionsDir(), getToolchainsDir(), getReposDir()}
	if ctx != nil && ctx.Paths != nil {
		roots = append(roots, ctx.Paths.DepsDir, ctx.Paths.CacheDir, ctx.Paths.ReposDir)
	}
	return roots
}

func canonicalInstallPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	parent := filepath.Dir(path)
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(resolved, filepath.Base(path))
	}
	return filepath.Clean(path)
}

func installPathAncestor(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func installOnePackage(ctx *RuntimeContext, name string, node *resolver.PackageNode, result *BuildResult, installer *build.ArtifactInstaller, globalValues map[string]any) error {
	values, err := pipeline.PackageConfigValues(ctx, name, globalValues)
	if err != nil {
		return err
	}

	installCtx := api.NewInstallContext(name, values)
	installCtx.SetOptions(maps.Clone(ctx.AllOptions[name]))
	installCtx.MergeGlobals(ctx.GlobalOptions, globalValues)

	node.Pkg.ExecInstallFuncs(result.PkgDirs[name].SourceDir, func(fn api.InstallFunc) {
		fn(installCtx)
	})

	buildCtx := result.BuildCtxs[name]
	if buildCtx == nil {
		return fmt.Errorf("package %s was not declared in this build", name)
	}

	installItems := installCtx.GetInstallItems()
	installItems = append(installItems, buildCtx.GetInstallItems()...)

	var installFilter api.InstallFilterFunc
	if installCtx.GetInstallFilter() != nil {
		installFilter = installCtx.GetInstallFilter()
	} else if buildCtx.GetInstallFilter() != nil {
		installFilter = buildCtx.GetInstallFilter()
	}

	prefix, prefixSet := installCtx.Prefix(), installCtx.PrefixSet()
	if prefixFlag != "" {
		prefix, prefixSet = "", false
	}

	installer.SetPackageInfo(name, &build.PkgInstallInfo{
		Prefix:        prefix,
		PrefixSet:     prefixSet,
		Targets:       result.AllTargets[name],
		InstallItems:  installItems,
		BuildDir:      result.PkgDirs[name].BuildDir,
		Mode:          result.Mode,
		TcName:        result.TcName,
		TargetOS:      node.Pkg.TargetOS(),
		BuildKey:      result.PkgBuildKeys[name],
		InstallFilter: installFilter,
	})
	return nil
}

func writeManifest(ctx *RuntimeContext, result *BuildResult, effectivePrefix string) error {
	base, err := manifestProjectDir(ctx)
	if err != nil {
		return err
	}
	var packages []installManifestEntry
	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if _, isSub := ctx.Resolver.SubParents()[name]; isSub {
			continue
		}
		if node.IsLocal() {
			sourceDir := result.PkgDirs[name].SourceDir
			relPath, err := filepath.Rel(base, sourceDir)
			if err != nil {
				return fmt.Errorf("manifest path for %s: %w", name, err)
			}
			if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
				return fmt.Errorf("local package %s source %q is outside project root %q", name, sourceDir, base)
			}
			gitDir := node.Pkg.SrcDir()
			packages = append(packages, installManifestEntry{
				Name:    name,
				Version: gitDescribe(gitDir),
				Source:  "local",
				Ref:     gitRevParse(gitDir),
				Path:    relPath,
			})
			continue
		}
		ip, ok := result.InstalledPkgs[name]
		if !ok {
			continue
		}
		entry := installManifestEntry{
			Name:    name,
			Version: ip.Version,
			Source:  "registry",
		}
		if node.IsNative() {
			entry.Source = "native"
			entry.URL = node.Native.GitURL
			if ref, ok := node.Native.Versions[ip.Version]; ok {
				entry.Ref = ref
			}
		} else if node.Pkg != nil {
			urls := node.Pkg.GitURLs()
			if len(urls) > 0 {
				entry.URL = urls[0]
			}
			if ref, ok := node.Pkg.Versions()[ip.Version]; ok {
				entry.Ref = ref
			}
		}
		packages = append(packages, entry)
	}
	mf := installManifest{
		VMake:     version.Version,
		Toolchain: result.TcName,
		Mode:      result.Mode,
		Generated: time.Now().UTC().Format(time.RFC3339),
		Packages:  packages,
	}
	path := filepath.Join(effectivePrefix, "manifest.json")
	return jsonio.Save(path, mf)
}

func gitDescribe(dir string) string {
	out, err := exec.RunWithOptions("git", gitcmd.Args("describe", "--tags", "--always", "--dirty"), exec.RunOptions{Dir: dir, Quiet: true})
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return gitRevParse(dir)
}

func gitRevParse(dir string) string {
	return repo.GitRevParse(dir)
}
