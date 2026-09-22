package plugin

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitcmd"
	"github.com/spock2300/vmake/internal/gitstore"
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
	return m.Store.Add(name, gitURL, cloneExtensionRepo)
}

func cloneExtensionRepo(gitURL, dir string) error {
	_, err := iexec.RunWithOptions("git", gitcmd.Args("clone", gitURL, dir), iexec.RunOptions{
		Env: map[string]string{"GIT_LFS_SKIP_SMUDGE": "1"}, Timeout: 5 * time.Minute, Quiet: true,
	})
	if err != nil {
		fs.RemoveIfExists(dir)
		return fmt.Errorf("git clone %s -> %s: %w", gitURL, dir, err)
	}
	return nil
}

func (m *Manager) UpdateRepo(name string) error {
	repoPath := m.Path(name)
	if !m.Exists(name) {
		return fmt.Errorf("extension repo '%s' not found", name)
	}
	_, err := iexec.RunWithOptions("git", gitcmd.Args("pull", "--ff-only"), iexec.RunOptions{
		Dir: repoPath, Env: map[string]string{"GIT_LFS_SKIP_SMUDGE": "1"}, Timeout: 2 * time.Minute, Quiet: true,
	})
	if err != nil {
		return fmt.Errorf("git pull in %s: %w", repoPath, err)
	}
	return nil
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
	output, err := iexec.RunWithEnvCaptured(repoPath, nil, "git", gitcmd.Args("config", "--get", "remote.origin.url")...)
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
	var failures []error

	repos := m.ListRepos()
	for _, r := range repos {
		names, err := fs.ListDirs(r.Path)
		if err != nil {
			failures = append(failures, err)
			continue
		}

		for _, name := range names {
			pluginDir := filepath.Join(r.Path, name)
			if !PluginInfoExists(pluginDir) {
				continue
			}

			info, err := LoadPluginInfo(pluginDir)
			if err != nil {
				failures = append(failures, fmt.Errorf("plugin %s: %w", pluginDir, err))
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

	return plugins, errors.Join(failures...)
}
