package repo

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitstore"
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

func (m *RepoManager) AddNative(name, urlTemplate string) error {
	repoPath := m.Path(name)
	if m.Exists(name) {
		return fmt.Errorf("'%s' already exists", name)
	}
	return SaveNativeConfig(repoPath, urlTemplate)
}

func (m *RepoManager) Remove(name string) error {
	return m.Store.Remove(name)
}

type RepoInfo struct {
	Name   string
	Native bool
}

func (m *RepoManager) ListInfo() []RepoInfo {
	names, _ := m.Store.List()
	result := make([]RepoInfo, 0, len(names))
	for _, name := range names {
		_, isNative, _ := LoadNativeConfig(m.Path(name))
		result = append(result, RepoInfo{Name: name, Native: isNative})
	}
	return result
}

func (m *RepoManager) Update(name string) error {
	repoPath := m.Path(name)
	if !m.Exists(name) {
		return fmt.Errorf("repo '%s' not found", name)
	}
	if _, isNative, _ := LoadNativeConfig(repoPath); isNative {
		return fmt.Errorf("native repo '%s' has no registry to update; use 'vmake pkg update <repo/name>' instead", name)
	}
	return FetchAndReset(repoPath)
}

func (m *RepoManager) IsNative(name string) bool {
	if !m.Exists(name) {
		return false
	}
	_, ok, _ := LoadNativeConfig(m.Path(name))
	return ok
}

func (m *RepoManager) GetNativeURL(name string) (string, error) {
	if !m.Exists(name) {
		return "", fmt.Errorf("repo '%s' not found", name)
	}
	cfg, ok, err := LoadNativeConfig(m.Path(name))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("repo '%s' is not a native repo", name)
	}
	return cfg.URL, nil
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
