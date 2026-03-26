package build

import (
	"fmt"
	"os"
	"path/filepath"

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
	InstallFilter api.InstallFilterFunc
}

type Installer struct {
	graph         *BuildGraph
	pkgDirs       map[string]string
	pkgInfo       map[string]*PkgInstallInfo
	defaultPrefix string
	installed     map[string]bool
}

func NewInstaller(graph *BuildGraph, pkgDirs map[string]string, defaultPrefix string) *Installer {
	return &Installer{
		graph:         graph,
		pkgDirs:       pkgDirs,
		pkgInfo:       make(map[string]*PkgInstallInfo),
		defaultPrefix: defaultPrefix,
		installed:     make(map[string]bool),
	}
}

func (i *Installer) SetPackageInfo(pkgName string, info *PkgInstallInfo) {
	i.pkgInfo[pkgName] = info
}

func (i *Installer) getEffectivePrefix(pkgName string, pkgInfo *PkgInstallInfo) string {
	prefix := pkgInfo.Prefix
	if !pkgInfo.PrefixSet {
		prefix = i.defaultPrefix
	}
	if prefix == "" {
		pkgDir := i.pkgDirs[pkgName]
		prefix = filepath.Join(pkgDir, "install")
	}
	return prefix
}

func (i *Installer) InstallAll() error {
	return i.graph.ForEachDefault(func(node *BuildNode) error {
		if err := i.installTarget(node); err != nil {
			return err
		}
		return i.installExtraItems(node)
	})
}

func (i *Installer) installTarget(node *BuildNode) error {
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
		return fmt.Errorf("package info not found: %s", pkgName)
	}

	prefix := i.getEffectivePrefix(pkgName, pkgInfo)

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

	if err := ensureDir(filepath.Dir(destPath)); err != nil {
		return err
	}

	if err := CopyFile(outputPath, destPath); err != nil {
		return err
	}

	i.installed[outputPath] = true
	return nil
}

func (i *Installer) installExtraItems(node *BuildNode) error {
	pkgName := node.PkgName
	pkgInfo, ok := i.pkgInfo[pkgName]
	if !ok {
		return nil
	}

	prefix := i.getEffectivePrefix(pkgName, pkgInfo)
	pkgDir := i.pkgDirs[pkgName]

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
			if err := ensureDir(filepath.Dir(destPath)); err != nil {
				return err
			}
			if err := CopyFile(srcPath, destPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func (i *Installer) getOutputPath(pkgName string, pkgInfo *PkgInstallInfo, node *BuildNode) string {
	name := targetFilename(node.Target.Kind(), node.Target.Name())

	buildDir := fmt.Sprintf("%s-%s", pkgInfo.TcName, pkgInfo.Mode)
	return filepath.Join(i.pkgDirs[pkgName], "build", buildDir, name)
}

func (i *Installer) getInstallPath(prefix string, target *api.Target) string {
	installDir := target.InstallDir()
	basename := targetFilename(target.Kind(), target.Name())

	if installDir != "" {
		return filepath.Join(prefix, installDir, basename)
	}

	switch target.Kind() {
	case api.TargetBinary:
		return filepath.Join(prefix, "bin", basename)
	case api.TargetStatic, api.TargetShared:
		return filepath.Join(prefix, "lib", basename)
	default:
		return filepath.Join(prefix, basename)
	}
}

func (i *Installer) copyDirWithFilter(src, dest string, filter api.InstallFilterFunc) error {
	var cf CopyFilter
	if filter != nil {
		cf = func(path string, isDir bool) bool {
			return filter(path, isDir)
		}
	}
	return CopyDirWithFilter(src, dest, cf)
}
