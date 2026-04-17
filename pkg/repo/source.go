package repo

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/flock"
	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/pkg/api"
)

type SourceManager struct {
	sourcesDir string
	globalDir  string
}

func NewSourceManager(sourcesDir, globalDir string) *SourceManager {
	return &SourceManager{sourcesDir: sourcesDir, globalDir: globalDir}
}

func (m *SourceManager) globalSrcPath(pkg *api.Package) string {
	return filepath.Join(m.globalDir, pkg.Repo, pkg.Name, "src")
}

func (m *SourceManager) localSrcPath(pkg *api.Package) string {
	return filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "src")
}

func (m *SourceManager) ensureSymlink(pkg *api.Package) error {
	return fs.EnsureSymlink(m.localSrcPath(pkg), m.globalSrcPath(pkg))
}

func (m *SourceManager) acquireLock(pkg *api.Package) (*flock.FileLock, error) {
	return flock.Acquire(filepath.Join(m.globalDir, pkg.Repo, pkg.Name))
}

func (m *SourceManager) EnsureSource(pkg *api.Package, version string) (string, error) {
	lock, err := m.acquireLock(pkg)
	if err != nil {
		return "", fmt.Errorf("acquire lock for %s: %w", pkg.FullName(), err)
	}
	defer lock.Release()

	globalDir := m.globalSrcPath(pkg)
	if err := fs.EnsureDir(globalDir); err != nil {
		return "", fmt.Errorf("ensure dir for %s: %w", pkg.FullName(), err)
	}

	tag := pkg.GetRef(version)
	if tag == "" {
		tag = version
	}

	if m.exists(globalDir) && m.exists(filepath.Join(globalDir, ".git")) {
		if IsAlreadyAtRef(globalDir, tag) {
			return m.finalize(pkg, globalDir)
		}
		if tag != "" {
			if FetchTags(globalDir) == nil && Checkout(globalDir, tag) == nil {
				return m.finalize(pkg, globalDir)
			}
		}
		fs.RemoveAll(globalDir)
		if err := fs.EnsureDir(globalDir); err != nil {
			return "", fmt.Errorf("recreate dir for %s: %w", pkg.FullName(), err)
		}
	}

	if err := m.ensureRepo(pkg, globalDir); err != nil {
		return "", err
	}

	if tag == "" {
		if err := m.ensureSymlink(pkg); err != nil {
			return "", err
		}
		return m.localSrcPath(pkg), nil
	}

	if err := m.retryWithFreshClone(pkg, globalDir, func() error {
		return FetchTags(globalDir)
	}); err != nil {
		return "", fmt.Errorf("fetch tags for %s: %w", pkg.FullName(), err)
	}

	if err := m.retryWithFreshClone(pkg, globalDir, func() error {
		return Checkout(globalDir, tag)
	}); err != nil {
		return "", fmt.Errorf("checkout %s failed for %s: %w", tag, pkg.FullName(), err)
	}

	return m.finalize(pkg, globalDir)
}

func (m *SourceManager) finalize(pkg *api.Package, dir string) (string, error) {
	if err := m.ensureSymlink(pkg); err != nil {
		return "", err
	}
	return m.localSrcPath(pkg), m.initSubmodules(pkg, dir)
}

func (m *SourceManager) initSubmodules(pkg *api.Package, dir string) error {
	if !pkg.Submodules() {
		return nil
	}
	if err := InitSubmodules(dir); err != nil {
		return fmt.Errorf("init submodules for %s: %w", pkg.FullName(), err)
	}
	return nil
}

func (m *SourceManager) retryWithFreshClone(pkg *api.Package, globalDir string, action func() error) error {
	if err := action(); err == nil {
		return nil
	}
	fs.RemoveAll(globalDir)
	if err := fs.EnsureDir(globalDir); err != nil {
		return err
	}
	if err := m.ensureRepo(pkg, globalDir); err != nil {
		return err
	}
	return action()
}

func (m *SourceManager) ensureRepo(pkg *api.Package, globalDir string) error {
	var lastErr error
	for _, url := range pkg.GitURLs() {
		lastErr = Clone(url, globalDir)
		if lastErr == nil {
			return nil
		}
		fs.RemoveIfExists(globalDir)
	}
	return fmt.Errorf("all mirrors failed for %s: %w", pkg.FullName(), lastErr)
}

func (m *SourceManager) UpdateSource(pkg *api.Package) error {
	lock, err := m.acquireLock(pkg)
	if err != nil {
		return fmt.Errorf("acquire lock for %s: %w", pkg.FullName(), err)
	}
	defer lock.Release()

	globalDir := m.globalSrcPath(pkg)

	if !m.exists(globalDir) || !m.exists(filepath.Join(globalDir, ".git")) {
		if err := fs.EnsureDir(globalDir); err != nil {
			return err
		}
		if err := m.ensureRepo(pkg, globalDir); err != nil {
			return err
		}
		return m.ensureSymlink(pkg)
	}

	if err := FetchAndReset(globalDir); err != nil {
		return err
	}
	return m.ensureSymlink(pkg)
}

func (m *SourceManager) CleanSource(repo, name string) error {
	localLink := filepath.Join(m.sourcesDir, repo, name, "src")
	_ = os.Remove(localLink)
	return fs.RemoveAll(filepath.Join(m.globalDir, repo, name))
}

func (m *SourceManager) exists(path string) bool {
	return fs.FileExists(path)
}
