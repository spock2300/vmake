package repo

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
)

type InstallConfig struct {
	Version string
	Options map[string]any
}

type Installer struct {
	sourceMgr   *SourceManager
	packagesDir string
	loader      *PackageLoader
	pkgs        map[string]*api.Package
}

func NewInstaller(sourceMgr *SourceManager, packagesDir, cacheDir string) *Installer {
	return &Installer{
		sourceMgr:   sourceMgr,
		packagesDir: packagesDir,
		loader:      NewPackageLoader(cacheDir),
		pkgs:        make(map[string]*api.Package),
	}
}

func (i *Installer) SetPackage(name string, pkg *api.Package) {
	i.pkgs[name] = pkg
}

func (i *Installer) Install(graph *DependencyGraph, configs map[string]*InstallConfig, tc *api.Toolchain) error {
	for _, name := range graph.Order {
		pkg, ok := graph.Packages[name]
		if !ok {
			return fmt.Errorf("package %s not found in graph", name)
		}

		config := configs[name]
		if config == nil {
			config = &InstallConfig{
				Version: "",
				Options: make(map[string]any),
			}
		}

		if err := i.installPackage(pkg, config, tc, graph); err != nil {
			return fmt.Errorf("failed to install %s: %w", name, err)
		}
	}

	return nil
}

func (i *Installer) installPackage(pkg *ResolvedPackage, config *InstallConfig, tc *api.Toolchain, graph *DependencyGraph) error {
	mode := "release"
	cacheHash := CacheHash(tc.CC, mode, config.Options)

	installDir := filepath.Join(i.packagesDir, pkg.Name, config.Version, cacheHash, "install")
	buildDir := filepath.Join(i.packagesDir, pkg.Name, config.Version, cacheHash, "build")

	if i.exists(installDir) {
		return nil
	}

	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return err
	}

	return nil
}

func (i *Installer) InstallPackage(pkgDef *PackageDef, config *InstallConfig, tc *api.Toolchain, graph *DependencyGraph, sourceDir string, configs map[string]*InstallConfig) error {
	mode := "release"
	cacheHash := CacheHash(tc.CC, mode, config.Options)

	installDir := filepath.Join(i.packagesDir, pkgDef.FullName(), config.Version, cacheHash, "install")
	buildDir := filepath.Join(i.packagesDir, pkgDef.FullName(), config.Version, cacheHash, "build")

	if i.hasInstalledFiles(installDir) {
		return nil
	}

	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return err
	}

	pkg := pkgDef.Package
	if pkg == nil {
		return fmt.Errorf("package definition has no Package loaded")
	}

	buildFunc := pkg.GetBuildFunc()
	if buildFunc == nil {
		return nil
	}

	opts := pkg.GetOptions()
	cfgVals := make(map[string]any)
	for name, opt := range opts {
		if opt.Default() != nil {
			cfgVals[name] = opt.Default()
		}
	}
	for k, v := range config.Options {
		cfgVals[k] = v
	}

	pkgCtx := api.NewPackageContext(pkgDef.Name, config.Version, tc, cfgVals)
	pkgCtx.SetOptions(opts)
	pkgCtx.SetDirs(sourceDir, buildDir, installDir)

	for _, depName := range graph.Order {
		if _, ok := graph.Packages[depName]; ok && depName != pkgDef.Name {
			depCfg := configs[depName]
			if depCfg == nil {
				depCfg = &InstallConfig{Version: "", Options: make(map[string]any)}
			}
			depInstallDir := i.GetInstallDir(depName, depCfg.Version, tc, depCfg.Options)
			if i.exists(depInstallDir) {
				pkgCtx.Deps()[depName] = api.NewInstalledPackage(depName, depCfg.Version, depInstallDir, nil)
			}
		}
	}

	buildFunc(pkgCtx)
	return nil
}

func (i *Installer) hasInstalledFiles(installDir string) bool {
	entries, err := os.ReadDir(installDir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

func (i *Installer) GetInstallDir(name, version string, tc *api.Toolchain, options map[string]any) string {
	mode := "release"
	cacheHash := CacheHash(tc.CC, mode, options)
	return filepath.Join(i.packagesDir, name, version, cacheHash, "install")
}

func (i *Installer) IsInstalled(name, version string, tc *api.Toolchain, options map[string]any) bool {
	installDir := i.GetInstallDir(name, version, tc, options)
	return i.hasInstalledFiles(installDir)
}

func (i *Installer) CleanBuild(name string) error {
	return os.RemoveAll(filepath.Join(i.packagesDir, name))
}

func (i *Installer) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (i *Installer) GetInstalledPackage(name, version string, tc *api.Toolchain, options map[string]any) *api.InstalledPackage {
	installDir := i.GetInstallDir(name, version, tc, options)
	if !i.hasInstalledFiles(installDir) {
		return nil
	}
	var libs []string
	if pkg, ok := i.pkgs[name]; ok {
		libs = pkg.Libs()
	}
	return api.NewInstalledPackage(name, version, installDir, libs)
}
