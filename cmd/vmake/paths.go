package main

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/lockfile"
)

var (
	cachedProjectDir string
	projectOverride  string
)

func findProjectDir() string {
	if cachedProjectDir != "" {
		return cachedProjectDir
	}
	if projectOverride != "" {
		found := validateProjectDir(projectOverride)
		cachedProjectDir = found
		return found
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
	if projectOverride != "" {
		return validateProjectDir(projectOverride)
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	found, _ := searchProjectDir(dir)
	return found
}

// validateProjectDir resolves and checks an explicit --project directory. The
// path is relative to the process cwd; it must exist and look like a project.
func validateProjectDir(override string) string {
	dir, err := filepath.Abs(override)
	if err != nil {
		fatalUsage("invalid --project directory %q: %v", override, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		fatalUsage("--project directory %q does not exist", dir)
	}
	if !info.IsDir() {
		fatalUsage("--project path %q is not a directory", dir)
	}
	if !hasProjectConfig(dir) && !hasBuildGo(dir) && !hasChildBuildGo(dir) {
		fatalUsage("--project %q is not a vmake project (no .vmake/, build.go or */build.go found)", dir)
	}
	return dir
}

// searchProjectDir locates the enclosing project root. A start directory that
// contains .vmake is itself a project. Otherwise the nearest ancestor that has
// both .vmake and build.go wins over a nested package build.go; an ancestor
// .vmake without its own build.go is only a container (for example a shared
// config directory above several standalone projects) and is ignored. Then a
// git root with its own build.go wins over the nearest package build.go: a
// fresh clone whose root build.go has no .vmake yet must not turn a package
// subdirectory into a nested project. Only after those searches does the
// nearest build.go win. Finally the start directory may still be a project
// root whose packages are child directories containing build.go.
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
		if hasGitRoot(current) && hasBuildGo(current) {
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

func hasGitRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
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
