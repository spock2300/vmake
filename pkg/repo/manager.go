package repo

import (
	"fmt"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/internal/gitstore"
)

type RepoManager struct {
	*gitstore.Store
}

func NewRepoManager(reposDir string) *RepoManager {
	return &RepoManager{Store: gitstore.New(reposDir)}
}

func (m *RepoManager) Add(name, gitURL string) error {
	return m.Store.Add(name, gitURL, Clone)
}

func (m *RepoManager) Remove(name string) error {
	return m.Store.Remove(name)
}

func (m *RepoManager) List() []string {
	names, _ := m.Store.List()
	return names
}

func (m *RepoManager) Update(name string) error {
	repoPath := m.Path(name)
	if !m.Exists(name) {
		return fmt.Errorf("repo '%s' not found", name)
	}
	return FetchAndReset(repoPath)
}

func (m *RepoManager) FindPackage(repo, name string) (string, error) {
	if len(name) == 0 {
		return "", fmt.Errorf("package name is empty")
	}

	firstChar := strings.ToLower(string(name[0]))
	pkgPath := filepath.Join(m.BaseDir(), repo, "packages", firstChar, name)

	if !fs.FileExists(pkgPath) {
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
	if !fs.FileExists(buildGo) {
		return "", fmt.Errorf("build.go not found in '%s'", pkgPath)
	}

	return buildGo, nil
}
