package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	vlog "github.com/spock2300/vmake/pkg/log"
)

const storageLayoutVersion = "4"

// cleanupLegacyStorage removes caches from older storage layouts once, on the
// first run after an upgrade. There is no migration: dependencies are
// re-materialized and rebuilt.
func cleanupLegacyStorage() {
	legacyGlobal := filepath.Join(vmakeDir, "sources")
	if fs.FileExists(legacyGlobal) {
		if err := os.RemoveAll(legacyGlobal); err != nil {
			vlog.Error("remove legacy source cache %s: %v", legacyGlobal, err)
		} else {
			vlog.Info("Removed legacy source cache %s (dependencies will be re-resolved)", legacyGlobal)
		}
	}

	// Layout v4 dropped the shared bare-mirror cache: sources are shallow
	// clones inside .vmake_deps. Mirrors are regenerable download caches, so
	// stale ones are deleted without migration.
	legacyMirrors := filepath.Join(getCacheDir(), "mirrors")
	if fs.FileExists(legacyMirrors) {
		if err := fs.RemoveAll(legacyMirrors); err != nil {
			vlog.Error("remove legacy mirror cache %s: %v", legacyMirrors, err)
		} else {
			vlog.Info("Removed legacy mirror cache %s", legacyMirrors)
		}
	}

	root := findProjectDirSoft()
	if root == "" {
		return
	}
	marker := filepath.Join(root, ".vmake", "layout")
	data, err := os.ReadFile(marker)
	markerVersion := strings.TrimSpace(string(data))
	if err == nil && markerVersion == storageLayoutVersion {
		return
	}
	// v4 keeps the single-tree .vmake_deps layout: trees are the only source
	// cache now and survive upgrades. The directory only exists in the
	// single-tree layout (v3+), so it is kept even when the marker was never
	// written (for example sources pre-downloaded by 'vmake pkg update' before
	// the first build). Pre-v3 directories are removed wholesale.
	legacyDirs := []string{filepath.Join(root, "vmake_deps")}
	treesDir := filepath.Join(root, ".vmake_deps")
	if markerVersion != "3" && !fs.FileExists(treesDir) {
		legacyDirs = append(legacyDirs, treesDir)
	}
	for _, legacy := range legacyDirs {
		if fs.FileExists(legacy) {
			if err := fs.RemoveAll(legacy); err != nil {
				vlog.Error("remove legacy %s: %v (will retry on next run)", legacy, err)
				return
			}
			vlog.Info("Removed legacy %s (storage layout upgraded; dependencies will be re-downloaded)", legacy)
		}
	}
	pruneLegacyCache(getCacheDir())
	if err := fs.EnsureDir(filepath.Join(root, ".vmake")); err != nil {
		vlog.Error("create .vmake dir: %v", err)
		return
	}
	if err := os.WriteFile(marker, []byte(storageLayoutVersion), 0644); err != nil {
		vlog.Error("write layout marker %s: %v (will re-check on next run)", marker, err)
	}
}

// pruneLegacyCache removes pre-v3 entries from the shared cache. Only known
// vmake-owned layout names are touched; unknown entries are left alone so
// VMAKE_CACHE pointing at an unrelated directory cannot lose data. Old
// per-repo source directories are simply abandoned with the old layout.
func pruneLegacyCache(cacheDir string) {
	for _, name := range []string{"v2", "_localgit"} {
		path := filepath.Join(cacheDir, name)
		if !fs.FileExists(path) {
			continue
		}
		if err := fs.RemoveAll(path); err != nil {
			vlog.Error("remove legacy cache entry %s: %v", path, err)
			continue
		}
		vlog.Info("Removed legacy cache entry %s (storage layout upgraded)", path)
	}
}
