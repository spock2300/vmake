package repo

import (
	"fmt"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/pkg/api"
)

type SourceManager struct {
	sourcesDir string
}

func NewSourceManager(sourcesDir string) *SourceManager {
	return &SourceManager{sourcesDir: sourcesDir}
}

func (m *SourceManager) EnsureSource(pkg *api.Package, version string) (string, error) {
	repoDir := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "src")

	tag := pkg.GetRef(version)
	if tag == "" {
		tag = version
	}

	if m.exists(repoDir) && m.exists(filepath.Join(repoDir, ".git")) {
		if IsAlreadyAtRef(repoDir, tag) {
			return repoDir, m.initSubmodules(pkg, repoDir)
		}
		if tag != "" {
			if FetchTags(repoDir) == nil && Checkout(repoDir, tag) == nil {
				return repoDir, m.initSubmodules(pkg, repoDir)
			}
		}
		fs.RemoveIfExists(repoDir)
	}

	if err := m.ensureRepo(pkg, repoDir); err != nil {
		return "", err
	}

	if tag == "" {
		return repoDir, nil
	}

	if err := m.retryWithFreshClone(pkg, repoDir, func() error {
		return FetchTags(repoDir)
	}); err != nil {
		return "", fmt.Errorf("fetch tags for %s: %w", pkg.FullName(), err)
	}

	if err := m.retryWithFreshClone(pkg, repoDir, func() error {
		return Checkout(repoDir, tag)
	}); err != nil {
		return "", fmt.Errorf("checkout %s failed for %s: %w", tag, pkg.FullName(), err)
	}

	return repoDir, m.initSubmodules(pkg, repoDir)
}

func (m *SourceManager) initSubmodules(pkg *api.Package, repoDir string) error {
	if !pkg.Submodules() {
		return nil
	}
	if err := InitSubmodules(repoDir); err != nil {
		return fmt.Errorf("init submodules for %s: %w", pkg.FullName(), err)
	}
	return nil
}

func (m *SourceManager) retryWithFreshClone(pkg *api.Package, repoDir string, action func() error) error {
	if err := action(); err == nil {
		return nil
	}
	fs.RemoveIfExists(repoDir)
	if err := m.ensureRepo(pkg, repoDir); err != nil {
		return err
	}
	return action()
}

func (m *SourceManager) ensureRepo(pkg *api.Package, repoDir string) error {
	var lastErr error
	for _, url := range pkg.GitURLs() {
		lastErr = Clone(url, repoDir)
		if lastErr == nil {
			return nil
		}
		fs.RemoveIfExists(repoDir)
	}
	return fmt.Errorf("all mirrors failed for %s: %w", pkg.FullName(), lastErr)
}

func (m *SourceManager) UpdateSource(pkg *api.Package) error {
	repoDir := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "src")

	if !m.exists(repoDir) {
		return m.ensureRepo(pkg, repoDir)
	}

	return FetchAndReset(repoDir)
}

func (m *SourceManager) CleanSource(repo, name string) error {
	return fs.RemoveAll(filepath.Join(m.sourcesDir, repo, name))
}

func (m *SourceManager) exists(path string) bool {
	return fs.FileExists(path)
}
