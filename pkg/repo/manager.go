package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type RepoManager struct {
	reposDir string
}

func NewRepoManager(reposDir string) *RepoManager {
	return &RepoManager{reposDir: reposDir}
}

func (m *RepoManager) Add(name, gitURL string) error {
	repoPath := filepath.Join(m.reposDir, name)
	if m.exists(repoPath) {
		return fmt.Errorf("repo '%s' already exists", name)
	}

	if err := os.MkdirAll(filepath.Dir(repoPath), 0755); err != nil {
		return fmt.Errorf("failed to create repo directory: %w", err)
	}

	if err := Clone(gitURL, repoPath); err != nil {
		return fmt.Errorf("failed to clone repo: %w", err)
	}

	return nil
}

func (m *RepoManager) Remove(name string) error {
	repoPath := filepath.Join(m.reposDir, name)
	if !m.exists(repoPath) {
		return fmt.Errorf("repo '%s' not found", name)
	}

	return os.RemoveAll(repoPath)
}

func (m *RepoManager) List() []string {
	repos := []string{}
	entries, err := os.ReadDir(m.reposDir)
	if err != nil {
		return repos
	}

	for _, entry := range entries {
		if entry.IsDir() {
			repos = append(repos, entry.Name())
		}
	}
	return repos
}

func (m *RepoManager) Update(name string) error {
	repoPath := filepath.Join(m.reposDir, name)
	if !m.exists(repoPath) {
		return fmt.Errorf("repo '%s' not found", name)
	}

	return FetchAndReset(repoPath)
}

func (m *RepoManager) FindPackage(repo, name string) (string, error) {
	if len(name) == 0 {
		return "", fmt.Errorf("package name is empty")
	}

	firstChar := strings.ToLower(string(name[0]))
	pkgPath := filepath.Join(m.reposDir, repo, "packages", firstChar, name)

	if !m.exists(pkgPath) {
		return "", fmt.Errorf("package '%s/%s' not found", repo, name)
	}

	return pkgPath, nil
}

func (m *RepoManager) FindPackageGo(repo, name string) (string, error) {
	pkgPath, err := m.FindPackage(repo, name)
	if err != nil {
		return "", err
	}

	buildGo := filepath.Join(pkgPath, "build.go")
	if !m.exists(buildGo) {
		return "", fmt.Errorf("build.go not found in '%s'", pkgPath)
	}

	return buildGo, nil
}

func (m *RepoManager) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
