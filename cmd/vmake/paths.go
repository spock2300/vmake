package main

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/lockfile"
)

var cachedProjectDir string

func findProjectDir() string {
	if cachedProjectDir != "" {
		return cachedProjectDir
	}
	dir, err := os.Getwd()
	if err != nil {
		fatalMsg("get working directory: %v", err)
	}
	checkSubdirs := true
	for {
		if hasProjectMarker(dir, checkSubdirs) {
			cachedProjectDir = dir
			return dir
		}
		checkSubdirs = false
		parent := filepath.Dir(dir)
		if parent == dir {
			fatalMsg("not inside a vmake project (no .vmake/, build.go or */build.go found in %s or any parent directory)", dir)
		}
		dir = parent
	}
}

func findProjectDirSoft() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	checkSubdirs := true
	for {
		if hasProjectMarker(dir, checkSubdirs) {
			return dir
		}
		checkSubdirs = false
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func hasProjectMarker(dir string, checkSubdirs bool) bool {
	marker := filepath.Join(dir, ".vmake")
	if marker == vmakeDir {
		return false
	}
	if _, err := os.Stat(marker); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "build.go")); err == nil {
		return true
	}
	if !checkSubdirs {
		return false
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, e.Name(), "build.go")); err == nil {
				return true
			}
		}
	}
	return false
}

func getDepsDir() string {
	return filepath.Join(findProjectDir(), "vmake_deps")
}

func getReposDir() string      { return filepath.Join(vmakeDir, "repos") }
func getExtensionsDir() string { return filepath.Join(vmakeDir, "extensions") }
func getToolchainsDir() string { return filepath.Join(vmakeDir, "toolchains") }

// getCacheDir returns the global content-addressed cache root. VMAKE_CACHE
// overrides it (used by tests to isolate shared build state).
func getCacheDir() string {
	if dir := os.Getenv("VMAKE_CACHE"); dir != "" {
		return dir
	}
	return filepath.Join(vmakeDir, "cache")
}

func getLocksDir() string { return filepath.Join(getCacheDir(), "_locks") }

func getLockfilePath() string {
	return filepath.Join(findProjectDir(), ".vmake", lockfile.LockfileName)
}
