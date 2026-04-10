package repo

import (
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

func (i *PackageInstaller) SetToolchain(tc *toolchain.Toolchain) {
	i.tc = tc
}

func (i *PackageInstaller) SetPackage(name string, pkg *api.Package) {
	i.pkgs[name] = pkg
}

func (i *PackageInstaller) CleanBuild(name string) error {
	return fs.RemoveAll(filepath.Join(i.packagesDir, name))
}
