package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/pkg/repo"
)

type Manager struct {
	extensionsDir string
	vmakeDir      string
}

func NewManager(vmakeDir string) *Manager {
	return &Manager{
		extensionsDir: filepath.Join(vmakeDir, "extensions"),
		vmakeDir:      vmakeDir,
	}
}

type ExtensionRepo struct {
	Name string
	Path string
	URL  string
}

func (m *Manager) AddRepo(name, gitURL string) error {
	repoPath := filepath.Join(m.extensionsDir, name)
	if m.exists(repoPath) {
		return fmt.Errorf("extension repo '%s' already exists", name)
	}

	if err := fs.EnsureDir(m.extensionsDir); err != nil {
		return fmt.Errorf("failed to create extensions directory: %w", err)
	}

	if err := repo.Clone(gitURL, repoPath); err != nil {
		return fmt.Errorf("failed to clone extension repo: %w", err)
	}

	return nil
}

func (m *Manager) UpdateRepo(name string) error {
	repoPath := filepath.Join(m.extensionsDir, name)
	if !m.exists(repoPath) {
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
	repoPath := filepath.Join(m.extensionsDir, name)
	if !m.exists(repoPath) {
		return fmt.Errorf("extension repo '%s' not found", name)
	}

	return fs.RemoveAll(repoPath)
}

func (m *Manager) ListRepos() []ExtensionRepo {
	var repos []ExtensionRepo

	entries, err := os.ReadDir(m.extensionsDir)
	if err != nil {
		return repos
	}

	for _, entry := range entries {
		if entry.IsDir() {
			repoPath := filepath.Join(m.extensionsDir, entry.Name())
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
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
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

func (m *Manager) exists(path string) bool {
	return fs.FileExists(path)
}
