package plugin

import (
	"fmt"
	"os"
	"path/filepath"

	iexec "gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/internal/gitstore"
	"gitee.com/spock2300/vmake/pkg/repo"
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

	if err := repo.Pull(repoPath); err != nil {
		return err
	}

	return m.clearCompiledPlugins(repoPath)
}

func (m *Manager) clearCompiledPlugins(repoPath string) error {
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		soPath := filepath.Join(repoPath, entry.Name(), "plugin.so")
		fs.RemoveIfExists(soPath)
	}
	return nil
}

func (m *Manager) RemoveRepo(name string) error {
	return m.Store.Remove(name)
}

func (m *Manager) ListRepos() []ExtensionRepo {
	var repos []ExtensionRepo

	entries, err := os.ReadDir(m.BaseDir())
	if err != nil {
		return repos
	}

	for _, entry := range entries {
		if entry.IsDir() {
			repoPath := filepath.Join(m.BaseDir(), entry.Name())
			url := m.getRepoURL(repoPath)
			repos = append(repos, ExtensionRepo{
				Name: entry.Name(),
				Path: repoPath,
				URL:  url,
			})
		}
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
		entries, err := os.ReadDir(r.Path)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			pluginDir := filepath.Join(r.Path, entry.Name())
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

func (m *Manager) CompilePlugin(pluginDir string, force bool) (string, error) {
	result := Compile(pluginDir, force)
	if !result.Success {
		return "", result.Error
	}
	return result.OutputPath, nil
}
