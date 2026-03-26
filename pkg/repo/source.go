package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/pkg/api"
)

func removeAllDir(path string) {
	os.RemoveAll(path)
}

type SourceManager struct {
	sourcesDir string
}

func NewSourceManager(sourcesDir string) *SourceManager {
	return &SourceManager{sourcesDir: sourcesDir}
}

func (m *SourceManager) EnsureSource(pkg *api.Package, version string) (string, error) {
	repoDir := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "repo")

	tag := pkg.GetRef(version)
	if tag == "" {
		tag = version
	}

	if m.exists(repoDir) && m.exists(filepath.Join(repoDir, ".git")) {
		if IsAlreadyAtRef(repoDir, tag) {
			return repoDir, nil
		}
		// Repo exists but at wrong ref — try fetch+checkout instead of re-cloning
		if tag != "" {
			if FetchTags(repoDir) == nil && Checkout(repoDir, tag) == nil {
				return repoDir, nil
			}
		}
		removeAllDir(repoDir)
	}

	if err := m.ensureRepo(pkg, repoDir); err != nil {
		return "", err
	}

	if tag == "" {
		return repoDir, nil
	}

	if pkg.Submodules() {
		_ = InitSubmodules(repoDir)
	}

	if err := FetchTags(repoDir); err != nil {
		removeAllDir(repoDir)
		if err := m.ensureRepo(pkg, repoDir); err != nil {
			return "", err
		}
		if err := FetchTags(repoDir); err != nil {
			return "", fmt.Errorf("fetch tags for %s: %w", pkg.FullName(), err)
		}
	}

	if err := Checkout(repoDir, tag); err != nil {
		removeAllDir(repoDir)
		if err := m.ensureRepo(pkg, repoDir); err != nil {
			return "", err
		}
		_ = FetchTags(repoDir)
		if err := Checkout(repoDir, tag); err != nil {
			return "", fmt.Errorf("checkout %s failed for %s: %w", tag, pkg.FullName(), err)
		}
	}

	return repoDir, nil
}

func (m *SourceManager) ensureRepo(pkg *api.Package, repoDir string) error {
	var lastErr error
	for _, url := range pkg.GitURLs() {
		lastErr = Clone(url, repoDir)
		if lastErr == nil {
			return nil
		}
		os.RemoveAll(repoDir)
	}
	return fmt.Errorf("all mirrors failed for %s: %w", pkg.FullName(), lastErr)
}

func (m *SourceManager) UpdateSource(pkg *api.Package) error {
	repoDir := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "repo")

	if !m.exists(repoDir) {
		var lastErr error
		for _, url := range pkg.GitURLs() {
			if err := Clone(url, repoDir); err == nil {
				return nil
			} else {
				lastErr = err
				os.RemoveAll(repoDir)
			}
		}
		return lastErr
	}

	return FetchAndReset(repoDir)
}

func (m *SourceManager) GetSourceDir(repo, name string) string {
	return filepath.Join(m.sourcesDir, repo, name, "repo")
}

func (m *SourceManager) HasSource(repo, name string) bool {
	repoDir := filepath.Join(m.sourcesDir, repo, name, "repo")
	return m.exists(repoDir) && m.exists(filepath.Join(repoDir, ".git"))
}

func (m *SourceManager) CleanSource(repo, name string) error {
	return os.RemoveAll(filepath.Join(m.sourcesDir, repo, name))
}

func (m *SourceManager) DistClean(repo, name string) error {
	repoDir := filepath.Join(m.sourcesDir, repo, name, "repo")
	cmd := exec.Command("make", "distclean")
	cmd.Dir = repoDir
	cmd.Run()
	return nil
}

func (m *SourceManager) exists(path string) bool {
	return fs.FileExists(path)
}
