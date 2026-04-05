package build

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/pkg/api"
	vlog "gitee.com/spock2300/vmake/pkg/log"
)

type PkgInstallInfo struct {
	Prefix        string
	PrefixSet     bool
	Targets       map[string]*api.Target
	InstallItems  []api.InstallItem
	BuildDir      string
	Mode          string
	TcName        string
	BuildKey      string
	InstallFilter api.InstallFilterFunc
}

type ArtifactInstaller struct {
	graph         *BuildGraph
	pkgDirs       map[string]*api.PkgDirs
	pkgInfo       map[string]*PkgInstallInfo
	defaultPrefix string
	installType   string
	installed     map[string]bool
}

func NewArtifactInstaller(graph *BuildGraph, pkgDirs map[string]*api.PkgDirs, defaultPrefix string) *ArtifactInstaller {
	return &ArtifactInstaller{
		graph:         graph,
		pkgDirs:       pkgDirs,
		pkgInfo:       make(map[string]*PkgInstallInfo),
		defaultPrefix: defaultPrefix,
		installType:   "runtime",
		installed:     make(map[string]bool),
	}
}

func (i *ArtifactInstaller) SetInstallType(t string) {
	i.installType = t
}

func (i *ArtifactInstaller) SetPackageInfo(pkgName string, info *PkgInstallInfo) {
	i.pkgInfo[pkgName] = info
}

func (i *ArtifactInstaller) isSDK() bool {
	return i.installType == "sdk"
}

func (i *ArtifactInstaller) getEffectivePrefix(pkgInfo *PkgInstallInfo) string {
	if pkgInfo.PrefixSet {
		return pkgInfo.Prefix
	}
	return i.defaultPrefix
}

func (i *ArtifactInstaller) InstallAll() error {
	return i.graph.ForEachDefault(func(node *BuildNode) error {
		if err := i.installTarget(node); err != nil {
			return err
		}
		return i.installExtraItems(node)
	})
}

func (i *ArtifactInstaller) installTarget(node *BuildNode) error {
	pkgName := node.PkgName
	target := node.Target

	if target.NoInstall() {
		return nil
	}

	kind := target.Kind()
	if kind == api.TargetObject {
		return nil
	}

	pkgInfo, ok := i.pkgInfo[pkgName]
	if !ok {
		return nil
	}

	if !i.isSDK() && kind == api.TargetStatic {
		vlog.Info("  SKIP %s (static lib, use --install-type sdk)", targetFilename(kind, target.Name()))
		return nil
	}

	prefix := i.getEffectivePrefix(pkgInfo)

	outputPath := i.getOutputPath(pkgName, pkgInfo, node)
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return nil
	}

	destPath := i.getInstallPath(prefix, target)

	if pkgInfo.InstallFilter != nil {
		if !pkgInfo.InstallFilter(outputPath, true) {
			vlog.Info("  SKIP %s (filtered)", filepath.Base(outputPath))
			return nil
		}
	}

	if i.installed[outputPath] {
		return nil
	}

	vlog.Info("  INSTALL %s -> %s", filepath.Base(outputPath), destPath)

	if err := fs.EnsureDir(filepath.Dir(destPath)); err != nil {
		return err
	}

	if err := CopyFile(outputPath, destPath); err != nil {
		return fmt.Errorf("install library failed: %w", err)
	}

	i.installed[outputPath] = true
	return i.installPublicIncludes(node, pkgInfo, prefix)
}

func copyPublicIncludes(target *api.Target, baseDir, includeDir string) error {
	for _, inc := range target.PublicIncludes() {
		srcPath := filepath.Join(baseDir, inc)
		info, err := os.Stat(srcPath)
		if err != nil {
			continue
		}

		if info.IsDir() {
			rule := target.IncludeRule(inc)
			if len(rule) > 0 {
				vlog.Info("  INSTALL DIR %s (match: %s) -> %s", inc, rule, includeDir)
				if err := CopyDirMatching(srcPath, includeDir, func(name string) bool {
					return MatchPatterns(rule, name)
				}); err != nil {
					return fmt.Errorf("install headers failed: %w", err)
				}
			} else {
				vlog.Info("  INSTALL DIR %s -> %s", inc, includeDir)
				if err := CopyDir(srcPath, includeDir); err != nil {
					return fmt.Errorf("install headers failed: %w", err)
				}
			}
		} else {
			rule := target.IncludeRule(inc)
			if len(rule) > 0 && !MatchPatterns(rule, filepath.Base(srcPath)) {
				continue
			}
			dest := filepath.Join(includeDir, filepath.Base(srcPath))
			vlog.Info("  INSTALL %s -> %s", filepath.Base(srcPath), dest)
			if err := fs.EnsureDir(includeDir); err != nil {
				return err
			}
			if err := CopyFile(srcPath, dest); err != nil {
				return fmt.Errorf("install header failed: %w", err)
			}
		}
	}

	return nil
}

func (i *ArtifactInstaller) installPublicIncludes(node *BuildNode, pkgInfo *PkgInstallInfo, prefix string) error {
	if !i.isSDK() {
		return nil
	}
	return copyPublicIncludes(node.Target, i.pkgDirs[node.PkgName].SourceDir, filepath.Join(prefix, "include"))
}

func (i *ArtifactInstaller) installExtraItems(node *BuildNode) error {
	pkgName := node.PkgName
	pkgInfo, ok := i.pkgInfo[pkgName]
	if !ok {
		return nil
	}

	prefix := i.getEffectivePrefix(pkgInfo)
	pkgDir := i.pkgDirs[pkgName].SourceDir

	for _, item := range pkgInfo.InstallItems {
		srcPath := filepath.Join(pkgDir, item.Src)
		destPath := filepath.Join(prefix, item.Dest)

		if pkgInfo.InstallFilter != nil {
			if !pkgInfo.InstallFilter(item.Src, false) {
				vlog.Info("  SKIP %s (filtered)", item.Src)
				continue
			}
		}

		info, err := os.Stat(srcPath)
		if os.IsNotExist(err) {
			vlog.Info("  SKIP %s (not found)", item.Src)
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", srcPath, err)
		}

		if info.IsDir() {
			vlog.Info("  INSTALL DIR %s -> %s", item.Src, destPath)
			if err := i.copyDirWithFilter(srcPath, destPath, pkgInfo.InstallFilter); err != nil {
				return err
			}
		} else {
			vlog.Info("  INSTALL %s -> %s", item.Src, destPath)
			if err := fs.EnsureDir(filepath.Dir(destPath)); err != nil {
				return err
			}
			if err := CopyFile(srcPath, destPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func (i *ArtifactInstaller) getOutputPath(pkgName string, pkgInfo *PkgInstallInfo, node *BuildNode) string {
	name := targetFilename(node.Target.Kind(), node.Target.Name())
	if pkgInfo.BuildDir != "" {
		return filepath.Join(pkgInfo.BuildDir, name)
	}
	return BuildPath(i.pkgDirs[pkgName].SourceDir, pkgInfo.BuildKey, name)
}

func (i *ArtifactInstaller) getInstallPath(prefix string, target *api.Target) string {
	installDir := target.InstallDir()
	basename := targetFilename(target.Kind(), target.Name())

	if installDir != "" {
		return filepath.Join(prefix, installDir, basename)
	}

	if dir := target.Kind().InstallDir(); dir != "" {
		return filepath.Join(prefix, dir, basename)
	}
	return filepath.Join(prefix, basename)
}

func (i *ArtifactInstaller) copyDirWithFilter(src, dest string, filter api.InstallFilterFunc) error {
	var cf CopyFilter
	if filter != nil {
		cf = func(path string, isDir bool) bool {
			return filter(path, isDir)
		}
	}
	return CopyDirWithFilter(src, dest, cf)
}
