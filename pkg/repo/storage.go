package repo

import (
	"path/filepath"

	internalfs "github.com/spock2300/vmake/internal/fs"
)

// CleanTree removes a package's working tree, state and per-build outputs.
func (m *SourceManager) CleanTree(key string) error {
	root, err := m.treePath(key)
	if err != nil {
		return err
	}
	if err := m.validateManagedPath(root); err != nil {
		return err
	}
	return internalfs.RemoveAll(root)
}

// CleanOutputs removes only the per-build outputs of a package, keeping the
// materialized source tree.
func (m *SourceManager) CleanOutputs(key string) error {
	root, err := m.treePath(key)
	if err != nil {
		return err
	}
	if err := m.validateManagedPath(root); err != nil {
		return err
	}
	return internalfs.RemoveAll(filepath.Join(root, "out"))
}

// CleanSource removes a package working tree. The shallow tree is rebuilt on
// demand; the shared git object cache under <cacheDir>/git is cross-project
// and is intentionally left untouched.
func (m *SourceManager) CleanSource(repoName, name string) error {
	key := PackageTreeKey(repoName, name)
	root, err := m.treePath(key)
	if err != nil {
		return err
	}
	if err := m.validateManagedPath(root); err != nil {
		return err
	}
	return internalfs.RemoveAll(root)
}

// CleanMaterializedSource removes a package's materialized working tree and
// its state file while keeping per-build outputs and own-Git third-party
// source trees.
func (m *SourceManager) CleanMaterializedSource(repoName, name string) error {
	key := PackageTreeKey(repoName, name)
	root, err := m.treePath(key)
	if err != nil {
		return err
	}
	if err := m.validateManagedPath(root); err != nil {
		return err
	}
	srcDir := filepath.Join(root, "src")
	if err := m.validateManagedPath(srcDir); err != nil {
		return err
	}
	if err := internalfs.RemoveAll(srcDir); err != nil {
		return err
	}
	return internalfs.RemoveAll(m.statePath(root))
}
