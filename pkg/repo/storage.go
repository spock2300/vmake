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
	return internalfs.RemoveAll(root)
}

// CleanOutputs removes only the per-build outputs of a package, keeping the
// materialized source tree.
func (m *SourceManager) CleanOutputs(key string) error {
	root, err := m.treePath(key)
	if err != nil {
		return err
	}
	return internalfs.RemoveAll(filepath.Join(root, "out"))
}

// CleanSource removes a package working tree. Sources are shallow clones
// materialized on demand, so there is no shared cache entry to clean up.
func (m *SourceManager) CleanSource(repoName, name string) error {
	key := PackageTreeKey(repoName, name)
	root, err := m.treePath(key)
	if err != nil {
		return err
	}
	return internalfs.RemoveAll(root)
}
