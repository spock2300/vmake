package repo

import (
	"encoding/json"
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
	cacheDir    string
	pkgs        map[string]*api.Package
	repoMgr     *RepoManager
	configs     map[string]*InstallConfig
	tc          *api.Toolchain
	installed   map[string]*api.InstalledPackage
}

func NewInstaller(sourceMgr *SourceManager, packagesDir, cacheDir string) *Installer {
	return &Installer{
		sourceMgr:   sourceMgr,
		packagesDir: packagesDir,
		cacheDir:    cacheDir,
		pkgs:        make(map[string]*api.Package),
		installed:   make(map[string]*api.InstalledPackage),
	}
}

func (i *Installer) SetRepoManager(mgr *RepoManager) {
	i.repoMgr = mgr
}

func (i *Installer) SetConfigs(configs map[string]*InstallConfig) {
	i.configs = configs
}

func (i *Installer) SetConfig(name string, cfg *InstallConfig) {
	if i.configs == nil {
		i.configs = make(map[string]*InstallConfig)
	}
	i.configs[name] = cfg
}

func (i *Installer) SetToolchain(tc *api.Toolchain) {
	i.tc = tc
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

	if i.hasInstalledFiles(installDir) {
		return nil
	}

	return fmt.Errorf("package %s not installed; run vmake build first", pkgDef.FullName())
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
	if pkg, ok := i.installed[name]; ok {
		return pkg
	}

	installDir := i.GetInstallDir(name, version, tc, options)
	if !i.hasInstalledFiles(installDir) {
		return nil
	}
	var libs []string
	if pkg, ok := i.pkgs[name]; ok {
		libs = pkg.Libs()
	}

	pkg := api.NewInstalledPackage(name, version, installDir, libs)
	pkg.Deps = i.loadDeps(installDir)
	i.installed[name] = pkg
	return pkg
}

func (i *Installer) loadDeps(installDir string) []string {
	metaPath := filepath.Join(installDir, "vmake.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil
	}

	var meta struct {
		Deps []string `json:"deps"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	return meta.Deps
}

func (i *Installer) saveDeps(installDir string, deps []string) {
	meta := struct {
		Deps []string `json:"deps"`
	}{Deps: deps}

	data, err := json.Marshal(meta)
	if err != nil {
		return
	}
	metaPath := filepath.Join(installDir, "vmake.json")
	os.WriteFile(metaPath, data, 0644)
}
