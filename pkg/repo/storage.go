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

func (m *SourceManager) CleanMaterializedSourceAt(root string) error {
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
