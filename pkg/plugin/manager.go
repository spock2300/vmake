package plugin

import (
	"fmt"
	"path/filepath"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitstore"
	"github.com/spock2300/vmake/pkg/repo"
)

type Manager struct {
	*gitstore.Store
	vmakeDir string
}

func NewManager(vmakeDir string) *Manager {
	return &Manager{
		Store:    gitstore.New(filepath.Join(vmakeDir, "extensions")),
		vmakeDir: vmakeDir,
	}
}

type ExtensionRepo struct {
	Name string
	Path string
	URL  string
}

func (m *Manager) AddRepo(name, gitURL string) error {
	return m.Store.Add(name, gitURL, repo.Clone)
}

func (m *Manager) UpdateRepo(name string) error {
	repoPath := m.Path(name)
	if !m.Exists(name) {
		return fmt.Errorf("extension repo '%s' not found", name)
	}
	return repo.Pull(repoPath)
}

func (m *Manager) RemoveRepo(name string) error {
	return m.Store.Remove(name)
}

func (m *Manager) ListRepos() []ExtensionRepo {
	var repos []ExtensionRepo

	names, err := fs.ListDirs(m.BaseDir())
	if err != nil {
		return repos
	}

	for _, name := range names {
		repoPath := filepath.Join(m.BaseDir(), name)
		url := m.getRepoURL(repoPath)
		repos = append(repos, ExtensionRepo{
			Name: name,
			Path: repoPath,
			URL:  url,
		})
	}

	return repos
}

func (m *Manager) getRepoURL(repoPath string) string {
	output, err := iexec.RunWithEnvCaptured(repoPath, nil, "git", "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}
	return iexec.TrimOutput(output)
}

type DiscoveredPlugin struct {
	RepoName   string
	PluginName string
	PluginDir  string
	Info       *Info
}

func (m *Manager) DiscoverPlugins() ([]DiscoveredPlugin, error) {
	var plugins []DiscoveredPlugin

	repos := m.ListRepos()
	for _, r := range repos {
		names, err := fs.ListDirs(r.Path)
		if err != nil {
			continue
		}

		for _, name := range names {
			pluginDir := filepath.Join(r.Path, name)
			if !PluginInfoExists(pluginDir) {
				continue
			}

			info, err := LoadPluginInfo(pluginDir)
			if err != nil {
				continue
			}

			if !info.Enabled {
				continue
			}

			plugins = append(plugins, DiscoveredPlugin{
				RepoName:   r.Name,
				PluginName: info.Name,
				PluginDir:  pluginDir,
				Info:       info,
			})
		}
	}

	return plugins, nil
}
