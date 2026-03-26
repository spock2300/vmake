package repo

import (
	"encoding/json"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
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

func (i *Installer) hasInstalledFiles(installDir string) bool {
	return fs.HasFiles(installDir)
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
	return fs.RemoveAll(filepath.Join(i.packagesDir, name))
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
