package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/flock"
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
	lock, err := m.LockRepo(name, true)
	if err != nil {
		return err
	}
	defer lock.Release()
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

// LockRepo acquires a repository-scoped lock. Extension loading holds a shared
// lock from the trust check through plugin loading; update/trust acquire the
// exclusive lock so the checked commit cannot change underneath the loader.
func (m *Manager) LockRepo(name string, exclusive bool) (*flock.FileLock, error) {
	if err := gitstore.ValidateName(name); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(name))
	lockPath := filepath.Join(m.vmakeDir, "_locks", "ext_"+hex.EncodeToString(sum[:])[:16]+".lock")
	if exclusive {
		return flock.Acquire(lockPath)
	}
	return flock.AcquireShared(lockPath)
}

func (m *Manager) UpdateRepo(name string) error {
	lock, err := m.LockRepo(name, true)
	if err != nil {
		return err
	}
	defer lock.Release()
	return m.UpdateRepoLocked(name)
}

// UpdateRepoLocked updates an extension repository while the caller holds its
// exclusive LockRepo lock.
func (m *Manager) UpdateRepoLocked(name string) error {
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
		if err := gitstore.ValidateName(name); err != nil {
			continue
		}
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

// URL returns the configured origin URL for an extension repository.
func (m *Manager) URL(name string) string {
	if !m.Exists(name) {
		return ""
	}
	return m.getRepoURL(m.Path(name))
}

// Commit returns the current HEAD of an extension repository.
func (m *Manager) Commit(name string) (string, error) {
	if !m.Exists(name) {
		return "", fmt.Errorf("extension repo '%s' not found", name)
	}
	output, err := iexec.RunWithEnvCaptured(m.Path(name), nil, "git", gitcmd.Args("rev-parse", "HEAD")...)
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD in %s: %w", m.Path(name), err)
	}
	commit := iexec.TrimOutput(output)
	if commit == "" {
		return "", fmt.Errorf("extension repo '%s' has no HEAD", name)
	}
	return commit, nil
}

// Clean reports whether an extension repository has no modified or untracked
// files. Ignored Go files are also rejected: MergeGoSources includes them in
// the interpreted program even though plain `git status` hides them. Trust
// records are only valid for the exact clean tree that was reviewed.
func (m *Manager) Clean(name string) (bool, error) {
	if !m.Exists(name) {
		return false, fmt.Errorf("extension repo '%s' not found", name)
	}
	output, err := iexec.RunWithEnvCaptured(m.Path(name), nil, "git", gitcmd.Args("status", "--porcelain", "--untracked-files=all")...)
	if err != nil {
		return false, fmt.Errorf("git status in %s: %w", m.Path(name), err)
	}
	if len(iexec.TrimOutput(output)) != 0 {
		return false, nil
	}
	ignored, err := iexec.RunWithEnvCaptured(m.Path(name), nil, "git", gitcmd.Args("ls-files", "--others", "--ignored", "--exclude-standard", "--", "*.go")...)
	if err != nil {
		return false, fmt.Errorf("git ls-files in %s: %w", m.Path(name), err)
	}
	return len(iexec.TrimOutput(ignored)) == 0, nil
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
