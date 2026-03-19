package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	for _, fullName := range i.graph.Order {
		node := i.graph.Nodes[fullName]
		if node == nil {
			return fmt.Errorf("target not found in graph: %s", fullName)
		}

		if !node.Target.IsDefault() {
			continue
		}

		if err := i.installTarget(node); err != nil {
			return err
		}

		if err := i.installExtraItems(node); err != nil {
			return err
		}
	}
	return nil
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

	if err := i.ensureDir(filepath.Dir(destPath)); err != nil {
		return err
	}

	if err := i.copyFile(outputPath, destPath); err != nil {
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
			if err := i.ensureDir(filepath.Dir(destPath)); err != nil {
				return err
			}
			if err := i.copyFile(srcPath, destPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func (i *Installer) getOutputPath(pkgName string, pkgInfo *PkgInstallInfo, node *BuildNode) string {
	var name string
	switch node.Target.Kind() {
	case api.TargetBinary:
		name = node.Target.Name()
	case api.TargetStatic:
		name = "lib" + node.Target.Name() + ".a"
	case api.TargetShared:
		name = "lib" + node.Target.Name() + ".so"
	case api.TargetObject:
		name = node.Target.Name() + ".o"
	default:
		name = node.Target.Name()
	}

	buildDir := fmt.Sprintf("%s-%s", pkgInfo.TcName, pkgInfo.Mode)
	return filepath.Join(i.pkgDirs[pkgName], "build", buildDir, name)
}

func (i *Installer) getInstallPath(prefix string, target *api.Target) string {
	var destPath string

	installDir := target.InstallDir()
	name := target.Name()
	kind := target.Kind()

	if installDir != "" {
		var basename string
		switch kind {
		case api.TargetBinary:
			basename = name
		case api.TargetStatic:
			basename = "lib" + name + ".a"
		case api.TargetShared:
			basename = "lib" + name + ".so"
		default:
			basename = name
		}
		destPath = filepath.Join(prefix, installDir, basename)
	} else {
		switch kind {
		case api.TargetBinary:
			destPath = filepath.Join(prefix, "bin", name)
		case api.TargetStatic:
			destPath = filepath.Join(prefix, "lib", "lib"+name+".a")
		case api.TargetShared:
			destPath = filepath.Join(prefix, "lib", "lib"+name+".so")
		default:
			destPath = filepath.Join(prefix, name)
		}
	}

	return destPath
}

func (i *Installer) ensureDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}

func (i *Installer) copyFile(src, dest string) error {
	return CopyFile(src, dest)
}

func (i *Installer) copyDir(src, dest string) error {
	return CopyDir(src, dest)
}

func (i *Installer) copyDirWithFilter(src, dest string, filter api.InstallFilterFunc) error {
	if err := i.ensureDir(dest); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		destPath := filepath.Join(dest, entry.Name())

		if entry.IsDir() {
			if err := i.copyDirWithFilter(srcPath, destPath, filter); err != nil {
				return err
			}
		} else {
			if strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			if filter != nil {
				if !filter(srcPath, false) {
					continue
				}
			}
			if err := i.copyFile(srcPath, destPath); err != nil {
				return err
			}
		}
	}

	return nil
}
