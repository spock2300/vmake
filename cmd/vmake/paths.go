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
	if found, ok := searchProjectDir(dir); ok {
		cachedProjectDir = found
		return found
	}
	root := dir
	for {
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	fatalUsage("not inside a vmake project (no .vmake/, build.go or */build.go found in %s or any parent directory)", root)
	return ""
}

func findProjectDirSoft() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	found, _ := searchProjectDir(dir)
	return found
}

// searchProjectDir locates the enclosing project root. A start directory that
// contains .vmake is itself a project. Otherwise the nearest ancestor that has
// both .vmake and build.go wins over a nested package build.go; an ancestor
// .vmake without its own build.go is only a container (for example a shared
// config directory above several standalone projects) and is ignored. Only
// after the upward walk finds no project .vmake does the nearest build.go win.
// Finally the start directory may still be a project root whose packages are
// child directories containing build.go.
func searchProjectDir(dir string) (string, bool) {
	start := dir
	for current := start; ; {
		if hasProjectConfig(current) && (current == start || hasBuildGo(current)) {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	for current := start; ; {
		if hasBuildGo(current) {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if hasChildBuildGo(start) {
		return start, true
	}
	return "", false
}

func hasProjectConfig(dir string) bool {
	marker := filepath.Join(dir, ".vmake")
	if marker == vmakeDir {
		return false
	}
	_, err := os.Stat(marker)
	return err == nil
}

func hasBuildGo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "build.go"))
	return err == nil
}

func hasChildBuildGo(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && hasBuildGo(filepath.Join(dir, e.Name())) {
			return true
		}
	}
	return false
}

// getDepsDir is the project-local single-source directory. Each package owns
// exactly one working tree under it (<repo>/<pkg>/src or local/<pkg>/src).
func getDepsDir() string {
	return filepath.Join(findProjectDir(), ".vmake_deps")
}

func getReposDir() string      { return filepath.Join(vmakeDir, "repos") }
func getExtensionsDir() string { return filepath.Join(vmakeDir, "extensions") }
func getToolchainsDir() string { return filepath.Join(vmakeDir, "toolchains") }

// getCacheDir returns the shared cache root (locks and lifecycle state).
// VMAKE_CACHE overrides it (used by tests to isolate shared build state).
func getCacheDir() string {
	if dir := os.Getenv("VMAKE_CACHE"); dir != "" {
		return dir
	}
	return filepath.Join(vmakeDir, "cache")
}

func getLockfilePath() string {
	return filepath.Join(findProjectDir(), ".vmake", lockfile.LockfileName)
}
