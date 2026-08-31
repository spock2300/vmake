package main

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/fs"
	vlog "github.com/spock2300/vmake/pkg/log"
)

const storageLayoutVersion = "2"

// cleanupLegacyStorage removes caches from older storage layouts once, on the
// first run after an upgrade. There is no migration: dependencies are
// re-resolved and rebuilt.
func cleanupLegacyStorage() {
	legacyGlobal := filepath.Join(vmakeDir, "sources")
	if fs.FileExists(legacyGlobal) {
		if err := os.RemoveAll(legacyGlobal); err != nil {
			vlog.Error("remove legacy source cache %s: %v", legacyGlobal, err)
		} else {
			vlog.Info("Removed legacy source cache %s (dependencies will be re-resolved)", legacyGlobal)
		}
	}

	root := findProjectDirSoft()
	if root == "" {
		return
	}
	marker := filepath.Join(root, ".vmake", "layout")
	if data, err := os.ReadFile(marker); err == nil && string(data) == storageLayoutVersion {
		return
	}
	deps := filepath.Join(root, "vmake_deps")
	if fs.FileExists(deps) {
		if err := os.RemoveAll(deps); err != nil {
			vlog.Error("remove legacy %s: %v (will retry on next run)", deps, err)
			return
		}
		vlog.Info("Removed legacy %s (storage layout upgraded; dependencies will be re-downloaded)", deps)
	}
	if err := fs.EnsureDir(filepath.Join(root, ".vmake")); err != nil {
		vlog.Error("create .vmake dir: %v", err)
		return
	}
	if err := os.WriteFile(marker, []byte(storageLayoutVersion), 0644); err != nil {
		vlog.Error("write layout marker %s: %v (will re-check on next run)", marker, err)
	}
}
