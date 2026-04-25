package repo

import (
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
)

type PackageInstaller struct {
	depsDir string
}

func NewPackageInstaller(depsDir string) *PackageInstaller {
	return &PackageInstaller{
		depsDir: depsDir,
	}
}

func (i *PackageInstaller) CleanBuild(name string) error {
	return fs.RemoveAll(filepath.Join(i.depsDir, name, "out"))
}
