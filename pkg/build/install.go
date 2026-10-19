package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

type PkgInstallInfo struct {
	Prefix        string
	PrefixSet     bool
	Targets       map[string]*api.Target
	InstallItems  []api.InstallItem
	BuildDir      string
	Mode          string
	TcName        string
	TargetOS      string
	BuildKey      string
	InstallFilter api.InstallFilterFunc
}

type ArtifactInstaller struct {
	ctx           context.Context
	graph         *BuildGraph
	pkgDirs       map[string]*api.PkgDirs
	pkgInfo       map[string]*PkgInstallInfo
	defaultPrefix string
	installType   string
	installed     map[string]bool
	files         []InstalledFile
}

type InstalledFile struct {
	Package     string
	Target      string
	Source      string
	Destination string
}

func (i *ArtifactInstaller) InstalledFiles() []InstalledFile {
	return append([]InstalledFile{}, i.files...)
}

func (i *ArtifactInstaller) recordFile(pkg, target, source, destination string) {
	i.files = append(i.files, InstalledFile{pkg, target, source, destination})
}

func copyArtifact(src, dest string, record func(string, string)) error {
	if err := fs.EnsureDir(filepath.Dir(dest)); err != nil {
		return err
	}
	vlog.Info("  INSTALL %s -> %s", filepath.Base(src), dest)
	if err := CopyFile(src, dest); err != nil {
		return err
	}
	if record != nil {
		record(src, dest)
	}
	return nil
}

func copyImportLibrary(artifactPath, destDir, targetOS string, kind api.TargetKind, record func(string, string)) error {
	implib := importLibraryPath(artifactPath, targetOS, kind)
	if implib == "" {
		return nil
	}
	if _, err := os.Stat(implib); err != nil {
		return nil
	}
	dest := filepath.Join(destDir, filepath.Base(implib))
	vlog.Info("  INSTALL %s -> %s", filepath.Base(implib), dest)
	if err := CopyFile(implib, dest); err != nil {
		return err
	}
	if record != nil {
		record(implib, dest)
	}
	return nil
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

func (i *ArtifactInstaller) InstallAll(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	i.ctx = ctx
	err := i.graph.ForEachDefault(true, func(node *BuildNode) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := i.installTarget(node); err != nil {
			return err
		}
		return i.installExtraItems(node)
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (i *ArtifactInstaller) installTarget(node *BuildNode) error {
	pkgName := node.PkgName
	target := node.Target

	if target.NoInstall() {
		return nil
	}

	if target.IsTest() {
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
	pkgDirs, err := i.packageDirs(pkgName)
	if err != nil {
		return err
	}

	if !i.isSDK() && kind == api.TargetStatic {
		vlog.Info("  SKIP %s (static lib, use --install-type sdk)", targetFilename(kind, target.Name(), pkgInfo.TargetOS))
		return nil
	}

	prefix := i.getEffectivePrefix(pkgInfo)

	outputPath := i.getOutputPath(pkgName, pkgInfo, node)
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return nil
	}

	destPath := i.getInstallPath(prefix, pkgInfo, target)

	if pkgInfo.InstallFilter != nil {
		if !pkgInfo.InstallFilter(outputPath, true) {
			vlog.Info("  SKIP %s (filtered)", filepath.Base(outputPath))
			return nil
		}
	}

	if i.installed[outputPath] {
		return nil
	}

	record := func(source, destination string) {
		i.recordFile(pkgName, target.Name(), source, destination)
	}
	if err := copyArtifact(outputPath, destPath, record); err != nil {
		return fmt.Errorf("install library failed: %w", err)
	}
	if err := copyImportLibrary(outputPath, filepath.Dir(destPath), pkgInfo.TargetOS, kind, record); err != nil {
		return fmt.Errorf("install import library failed: %w", err)
	}

	i.installed[outputPath] = true

	for _, outFile := range postLinkOutputPaths(target, pkgDirs.SourceDir, outputPath) {
		if err := i.ctx.Err(); err != nil {
			return err
		}
		if _, err := os.Stat(outFile); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("install post-link output %s: %w", outFile, err)
			}
			continue
		}
		postDest := filepath.Join(filepath.Dir(destPath), filepath.Base(outFile))
		vlog.Info("  INSTALL %s -> %s", filepath.Base(outFile), postDest)
		if err := CopyFile(outFile, postDest); err != nil {
			return fmt.Errorf("install post-link output failed: %w", err)
		}
		i.recordFile(pkgName, target.Name(), outFile, postDest)
	}

	return i.installPublicIncludes(node, pkgInfo, prefix)
}

func copyPublicIncludes(target *api.Target, baseDir, buildDir, includeDir string, record func(string, string)) error {
	for _, inc := range target.PublicIncludes() {
		srcPath := resolvePkgPath(baseDir, buildDir, inc)
		info, err := os.Stat(srcPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("stat public include %s: %w", srcPath, err)
			}
			continue
		}

		if info.IsDir() {
			rule := target.IncludeRule(inc)
			var filter CopyFilter
			if len(rule) > 0 {
				vlog.Info("  INSTALL DIR %s (match: %s) -> %s", inc, rule, includeDir)
				filter = func(path string, isDir bool) bool {
					if isDir {
						return true
					}
					return MatchPatterns(rule, filepath.Base(path))
				}
			} else {
				vlog.Info("  INSTALL DIR %s -> %s", inc, includeDir)
			}
			if err := copyDirWithFilter(srcPath, includeDir, filter, record); err != nil {
				return fmt.Errorf("install headers failed: %w", err)
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
			if record != nil {
				record(srcPath, dest)
			}
			if err := CopyFile(srcPath, dest); err != nil {
				return fmt.Errorf("install header failed: %w", err)
			}
		}
	}

	return nil
}

func (i *ArtifactInstaller) packageDirs(pkgName string) (*api.PkgDirs, error) {
	dirs := i.pkgDirs[pkgName]
	if dirs == nil || dirs.SourceDir == "" {
		return nil, fmt.Errorf("install %s: package directories are not resolved", pkgName)
	}
	return dirs, nil
}

func (i *ArtifactInstaller) installPublicIncludes(node *BuildNode, pkgInfo *PkgInstallInfo, prefix string) error {
	if !i.isSDK() {
		return nil
	}
	dirs := i.pkgDirs[node.PkgName]
	buildDir := pkgInfo.BuildDir
	if buildDir == "" && dirs != nil {
		buildDir = dirs.BuildDir
	}
	return copyPublicIncludes(node.Target, dirs.SourceDir, buildDir, filepath.Join(prefix, "include"), func(source, destination string) {
		i.recordFile(node.PkgName, node.Target.Name(), source, destination)
	})
}

func (i *ArtifactInstaller) installExtraItems(node *BuildNode) error {
	pkgName := node.PkgName
	pkgInfo, ok := i.pkgInfo[pkgName]
	if !ok {
		return nil
	}
	pkgDirs, err := i.packageDirs(pkgName)
	if err != nil {
		return err
	}

	prefix := i.getEffectivePrefix(pkgInfo)
	pkgDir := pkgDirs.SourceDir
	buildDir := pkgInfo.BuildDir
	if buildDir == "" {
		buildDir = pkgDirs.BuildDir
	}

	for _, item := range pkgInfo.InstallItems {
		if err := i.ctx.Err(); err != nil {
			return err
		}
		srcPath := resolvePkgPath(pkgDir, buildDir, item.Src)
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
			if err := copyDirWithFilter(srcPath, destPath, CopyFilter(pkgInfo.InstallFilter), func(source, destination string) { i.recordFile(pkgName, "", source, destination) }); err != nil {
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
			i.recordFile(pkgName, "", srcPath, destPath)
		}
	}

	return nil
}

func (i *ArtifactInstaller) getOutputPath(pkgName string, pkgInfo *PkgInstallInfo, node *BuildNode) string {
	name := targetFilename(node.Target.Kind(), node.Target.Name(), pkgInfo.TargetOS)
	if pkgInfo.BuildDir != "" {
		return filepath.Join(pkgInfo.BuildDir, name)
	}
	return BuildPath(i.pkgDirs[pkgName].SourceDir, pkgInfo.BuildKey, name)
}

func (i *ArtifactInstaller) getInstallPath(prefix string, pkgInfo *PkgInstallInfo, target *api.Target) string {
	installDir := target.InstallDir()
	basename := targetFilename(target.Kind(), target.Name(), pkgInfo.TargetOS)

	if installDir != "" {
		return filepath.Join(prefix, installDir, basename)
	}

	if dir := target.Kind().InstallDir(); dir != "" {
		return filepath.Join(prefix, dir, basename)
	}
	return filepath.Join(prefix, basename)
}
