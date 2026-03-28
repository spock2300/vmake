package repo

import (
	"encoding/json"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/config"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type PackageInstaller struct {
	sourceMgr   *SourceManager
	packagesDir string
	cacheDir    string
	pkgs        map[string]*api.Package
	repoMgr     *RepoManager
	configs     map[string]*config.EntryConfig
	tc          *toolchain.Toolchain
}

func NewPackageInstaller(sourceMgr *SourceManager, packagesDir, cacheDir string) *PackageInstaller {
	return &PackageInstaller{
		sourceMgr:   sourceMgr,
		packagesDir: packagesDir,
		cacheDir:    cacheDir,
		pkgs:        make(map[string]*api.Package),
	}
}

func (i *PackageInstaller) SetRepoManager(mgr *RepoManager) {
	i.repoMgr = mgr
}

func (i *PackageInstaller) SetConfigs(configs map[string]*config.EntryConfig) {
	i.configs = configs
}

func (i *PackageInstaller) SetConfig(name string, cfg *config.EntryConfig) {
	if i.configs == nil {
		i.configs = make(map[string]*config.EntryConfig)
	}
	i.configs[name] = cfg
}

func (i *PackageInstaller) SetToolchain(tc *toolchain.Toolchain) {
	i.tc = tc
}

func (i *PackageInstaller) SetPackage(name string, pkg *api.Package) {
	i.pkgs[name] = pkg
}

func (i *PackageInstaller) hasInstalledFiles(installDir string) bool {
	return fs.HasFiles(installDir)
}

func (i *PackageInstaller) GetInstallDir(name, version string, tc *toolchain.Toolchain, options map[string]any) string {
	mode := "release"
	cacheHash := CacheHash(tc.Tools.CC, mode, options)
	return filepath.Join(i.packagesDir, name, version, cacheHash, "install")
}

func (i *PackageInstaller) IsInstalled(name, version string, tc *toolchain.Toolchain, options map[string]any) bool {
	installDir := i.GetInstallDir(name, version, tc, options)
	return i.hasInstalledFiles(installDir)
}

func (i *PackageInstaller) CleanBuild(name string) error {
	return fs.RemoveAll(filepath.Join(i.packagesDir, name))
}

func (i *PackageInstaller) loadDeps(installDir string) []string {
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
