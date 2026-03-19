package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type SourceManager struct {
	sourcesDir string
}

func NewSourceManager(sourcesDir string) *SourceManager {
	return &SourceManager{sourcesDir: sourcesDir}
}

func (m *SourceManager) EnsureSource(pkg *PackageDef, version string) (string, error) {
	repoDir := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "repo")

	if !m.exists(repoDir) || !m.exists(filepath.Join(repoDir, ".git")) {
		var lastErr error
		for _, url := range pkg.GitURLs {
			if err := Clone(url, repoDir); err == nil {
				break
			} else {
				lastErr = err
				os.RemoveAll(repoDir)
			}
		}
		if lastErr != nil {
			return "", fmt.Errorf("all mirrors failed for %s: %w", pkg.FullName(), lastErr)
		}
	}

	_ = InitSubmodules(repoDir)

	_ = FetchTags(repoDir)

	tag := pkg.GetRef(version)
	if tag == "" {
		tag = version
	}
	if tag == "" {
		return "", fmt.Errorf("no version or tag available for %s", pkg.FullName())
	}

	currentTag, _ := GetCurrentTag(repoDir)
	if currentTag == tag {
		return repoDir, nil
	}

	if err := Checkout(repoDir, tag); err != nil {
		return "", fmt.Errorf("checkout %s failed for %s: %w", tag, pkg.FullName(), err)
	}

	return repoDir, nil
}

func (m *SourceManager) UpdateSource(pkg *PackageDef) error {
	repoDir := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "repo")

	if !m.exists(repoDir) {
		var lastErr error
		for _, url := range pkg.GitURLs {
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
	_, err := os.Stat(path)
	return err == nil
}
