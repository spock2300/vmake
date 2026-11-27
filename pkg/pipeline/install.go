package pipeline

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/resolver"
)

const InstallOwnershipMarker = ".vmake-install"

type InstallOptions struct {
	Prefix       string
	InstallType  string
	StorageRoots []string
}

func RunInstall(ctx *RuntimeContext, result *BuildResult, opts InstallOptions) error {
	if ctx.Context != nil && ctx.Context.Err() != nil {
		return ctx.Context.Err()
	}

	projectDir := ""
	if ctx.Paths != nil {
		projectDir = ctx.Paths.ProjectDir
	}
	effectivePrefix := opts.Prefix
	if effectivePrefix == "" {
		effectivePrefix = filepath.Join(projectDir, "install")
	}

	vlog.Info("")
	vlog.Info("Installing...")

	if err := replaceInstallPrefix(ctx.Context, effectivePrefix, projectDir, opts.StorageRoots, func() error {
		return installIntoPrefix(ctx, result, opts, effectivePrefix)
	}); err != nil {
		return err
	}
	vlog.Info("")
	vlog.Info("Install succeeded!")
	return nil
}

func installIntoPrefix(ctx *RuntimeContext, result *BuildResult, opts InstallOptions, effectivePrefix string) (err error) {
	defer scriptcall.Recover(&err)
	projectDir := ""
	if ctx.Paths != nil {
		projectDir = ctx.Paths.ProjectDir
	}
	defaultPrefix := ""
	if projectDir != "" {
		defaultPrefix = CanonicalInstallPath(filepath.Join(projectDir, "install"))
	}
	targetPrefix := ""
	if absolute, err := filepath.Abs(effectivePrefix); err == nil {
		targetPrefix = CanonicalInstallPath(absolute)
	} else {
		targetPrefix = CanonicalInstallPath(effectivePrefix)
	}
	if defaultPrefix == "" || (targetPrefix != defaultPrefix && !InstallPathAncestor(defaultPrefix, targetPrefix)) {
		if err := os.WriteFile(filepath.Join(effectivePrefix, InstallOwnershipMarker), []byte("vmake\n"), 0644); err != nil {
			return fmt.Errorf("write install prefix marker: %w", err)
		}
	}

	installer := build.NewArtifactInstaller(result.Graph, result.PkgDirs, effectivePrefix)
	installer.SetInstallType(opts.InstallType)

	for _, name := range ctx.DepGraph.Order {
		if ctx.Context != nil && ctx.Context.Err() != nil {
			return ctx.Context.Err()
		}
		node := ctx.DepGraph.Packages[name]
		if node.Pkg == nil || result.BuildCtxs[name] == nil || result.SubGraphRoots[name] {
			continue
		}
		if err := InstallOnePackage(ctx, name, node, result, installer, result.GlobalValues, ctx.allOptions[name], opts.Prefix); err != nil {
			return err
		}
	}

	if ctx.Context != nil && ctx.Context.Err() != nil {
		return ctx.Context.Err()
	}
	if err := installer.InstallAll(ctx.Context); err != nil {
		return err
	}

	if err := WriteManifest(ctx, result, effectivePrefix); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	if ctx.Context != nil && ctx.Context.Err() != nil {
		return ctx.Context.Err()
	}
	result.InstallFiles = installer.InstalledFiles()
	return nil
}

func InstallOnePackage(ctx *RuntimeContext, name string, node *resolver.PackageNode, result *BuildResult, installer *build.ArtifactInstaller, globalValues map[string]any, options map[string]*api.Option, prefixOverride string) error {
	values, err := PackageConfigValues(ctx, name, globalValues)
	if err != nil {
		return err
	}

	if options == nil && ctx != nil {
		options = ctx.allOptions[name]
	}
	installCtx := api.NewInstallContext(name, values)
	installCtx.SetOptions(maps.Clone(options))
	installCtx.MergeGlobals(
		visibleGlobalOptionsFor(node, options, ctx.globalOptions),
		visibleGlobalValuesFor(node, options, ctx.globalOptions, globalValues),
	)

	api.BindPackageExecInstallFuncs(node.Pkg, result.PkgDirs[name].SourceDir, func(fn api.InstallFunc) {
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
	if prefixOverride != "" {
		prefix, prefixSet = "", false
	}

	placements := make(map[string]build.ArtifactPlacement)
	for targetName := range result.AllTargets[name] {
		if placement, ok := result.TargetPlacements[name+":"+targetName]; ok {
			placements[targetName] = placement
		}
	}
	buildDir := ""
	if layout := result.Layouts[name]; layout != nil {
		buildDir = layout.Dirs.BuildDir
	}
	installer.SetPackageInfo(name, &build.PkgInstallInfo{
		Prefix:        prefix,
		PrefixSet:     prefixSet,
		Targets:       result.AllTargets[name],
		InstallItems:  installItems,
		BuildDir:      buildDir,
		Mode:          result.Mode,
		TcName:        result.TcName,
		TargetOS:      node.Pkg.TargetOS(),
		Placements:    placements,
		InstallFilter: installFilter,
	})
	return nil
}

func ValidateInstallPrefix(prefix, projectDir string, storageRoots ...string) error {
	target, defaultInstall, _, err := validateInstallPrefixPath(prefix, projectDir, storageRoots...)
	if err != nil {
		return err
	}
	return validateInstallPrefixOwnership(target, defaultInstall)
}

func validateInstallPrefixPath(prefix, projectDir string, storageRoots ...string) (target, defaultInstall, projectRoot string, err error) {
	if strings.TrimSpace(prefix) == "" {
		return "", "", "", fmt.Errorf("install prefix is empty")
	}
	absolute, err := filepath.Abs(prefix)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve install prefix %q: %w", prefix, err)
	}
	target = CanonicalInstallPath(absolute)
	if filepath.Dir(target) == target {
		return "", "", "", fmt.Errorf("refusing to clean filesystem root %q as install prefix", target)
	}
	if linkInfo, lstatErr := os.Lstat(absolute); lstatErr == nil {
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			info, statErr := os.Stat(absolute)
			if statErr != nil || !info.IsDir() {
				return "", "", "", fmt.Errorf("refusing to clean install prefix %q: it is not a directory", target)
			}
		} else if !linkInfo.IsDir() {
			return "", "", "", fmt.Errorf("refusing to clean install prefix %q: it is not a directory", target)
		}
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
	if projectDir != "" {
		if absProject, err := filepath.Abs(projectDir); err == nil {
			projectRoot = CanonicalInstallPath(absProject)
		}
	}
	for _, path := range protected {
		absProtected, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		path = CanonicalInstallPath(absProtected)
		if target == path || InstallPathAncestor(target, path) {
			return "", "", "", fmt.Errorf("refusing to clean %q as install prefix: it is %q or one of its ancestors", target, path)
		}
	}
	for _, path := range storageRoots {
		if strings.TrimSpace(path) == "" {
			continue
		}
		absStorage, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		path = CanonicalInstallPath(absStorage)
		if target == path || InstallPathAncestor(path, target) || InstallPathAncestor(target, path) {
			return "", "", "", fmt.Errorf("refusing to clean %q as install prefix: it overlaps vmake storage %q", target, path)
		}
	}
	if projectRoot != "" {
		defaultInstall = CanonicalInstallPath(filepath.Join(projectRoot, "install"))
		if target != defaultInstall && !InstallPathAncestor(defaultInstall, target) && InstallPathAncestor(projectRoot, target) {
			return "", "", "", fmt.Errorf("refusing to clean %q as install prefix: it is inside project root %q; use the default install directory or a path outside the project", target, projectRoot)
		}
	}
	return target, defaultInstall, projectRoot, nil
}

func validateInstallPrefixOwnership(target, defaultInstall string) error {
	if target != defaultInstall && !InstallPathAncestor(defaultInstall, target) {
		if info, err := os.Stat(target); err == nil && info.IsDir() && !installPrefixEmpty(target) && !fs.FileExists(filepath.Join(target, InstallOwnershipMarker)) {
			return fmt.Errorf("refusing to clean non-empty custom install prefix %q: use a new/empty directory or remove it manually; vmake only reuses directories carrying %s", target, InstallOwnershipMarker)
		}
	}
	return nil
}

func installPrefixEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

func CanonicalInstallPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	parent := filepath.Dir(path)
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(resolved, filepath.Base(path))
	}
	return filepath.Clean(path)
}

func InstallPathAncestor(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
