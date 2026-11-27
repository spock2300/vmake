package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
)

var (
	cachedProjectDir string
	projectOverride  string
)

func findProjectDir() string {
	if cachedProjectDir == "" {
		root, err := resolveProjectRoot()
		fatalResolveRoot(err)
		cachedProjectDir = root
	}
	return cachedProjectDir
}

func findProjectDirSoft() string {
	if projectOverride != "" {
		dir, err := validateProjectDirErr(projectOverride)
		if err != nil {
			fatalUsage("%v", err)
		}
		return dir
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	found, ok := searchProjectDir(canonicalOrSelf(dir))
	if !ok {
		return ""
	}
	return canonicalOrSelf(found)
}

var errNotInProject = errors.New("not inside a vmake project (no vmake.json, .vmake/, build.go or */build.go found in the current directory or any parent directory)")

func resolveProjectRoot() (string, error) {
	if projectOverride != "" {
		return resolveProjectRootFrom("", projectOverride)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return resolveProjectRootFrom(cwd, "")
}

func resolveProjectRootFrom(start, override string) (string, error) {
	if override != "" {
		return validateProjectDirErr(override)
	}
	found, ok := searchProjectDir(canonicalOrSelf(start))
	if !ok {
		return "", errNotInProject
	}
	// Package discovery uses physical paths. Give locks, build actions and the
	// install manifest the same root even when entered via a Windows 8.3 alias.
	found, err := fs.CanonicalPath(found)
	if err != nil {
		return "", err
	}
	if err := shadowedProjectRootErr(found, ""); err != nil {
		return "", err
	}
	return found, nil
}

func canonicalOrSelf(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if canonical, err := fs.CanonicalPath(path); err == nil {
		return canonical
	}
	return path
}

// rejectShadowedProjectRoot fails when the resolved project root is a nested
// .vmake marker while an enclosing project also exists. Such a package-local
// marker silently hides the real project, so require an explicit -C/--project
// root (or removal of the marker) instead of using the nested root. Calling
// from a descendant still reports the selected nested root. findProjectDirSoft
// and doctor deliberately skip this check to keep diagnosing the nested root.
func rejectShadowedProjectRoot(root string) {
	if err := shadowedProjectRootErr(root, projectOverride); err != nil {
		vlog.Error("%v", err)
		exitWithError(err)
	}
}

func shadowedProjectRootErr(root, override string) error {
	if override != "" || !hasProjectConfig(root) {
		return nil
	}
	enclosing, ok := findEnclosingProjectRoot(root)
	if !ok || filepath.Clean(enclosing) == filepath.Clean(root) {
		return nil
	}
	marker := filepath.Join(root, ".vmake")
	return &usageError{fmt.Errorf("project root %s has a .vmake marker shadowing the enclosing project %s; run vmake -C %s ... or remove %s", root, enclosing, enclosing, marker)}
}

// rejectShadowedProjectRootFromCWD applies the shadowed-root check before a
// command replaces os.Stdout/os.Stderr (inspect does this while resolving), so
// the usage error stays visible.
func rejectShadowedProjectRootFromCWD() {
	if projectOverride != "" {
		return
	}
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	if found, ok := searchProjectDir(canonicalOrSelf(dir)); ok {
		rejectShadowedProjectRoot(canonicalOrSelf(found))
	}
}

// validateProjectDirErr resolves and checks an explicit --project directory. The
// path is relative to the process cwd; it must exist and look like a project.
func validateProjectDirErr(override string) (string, error) {
	dir, err := filepath.Abs(override)
	if err != nil {
		return "", &usageError{fmt.Errorf("invalid --project directory %q: %v", override, err)}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", &usageError{fmt.Errorf("--project directory %q does not exist", dir)}
	}
	if !info.IsDir() {
		return "", &usageError{fmt.Errorf("--project path %q is not a directory", dir)}
	}
	dir, err = fs.CanonicalPath(dir)
	if err != nil {
		return "", err
	}
	if !hasProjectConfig(dir) && !hasManifest(dir) && !hasBuildGo(dir) && !hasChildBuildGo(dir) {
		return "", &usageError{fmt.Errorf("--project %q is not a vmake project (no vmake.json, .vmake/, build.go or */build.go found)", dir)}
	}
	if !hasProjectConfig(dir) && !hasManifest(dir) {
		if enclosing, ok := findEnclosingProjectRoot(dir); ok && filepath.Clean(enclosing) != filepath.Clean(dir) {
			return "", &usageError{fmt.Errorf("--project %q is a package inside project %q; use --project %q or create a .vmake marker in %q for an intentional nested project", dir, enclosing, enclosing, dir)}
		}
	}
	return dir, nil
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
	return searchProjectDirFrom(dir, false)
}

// searchProjectDirFrom implements the upward project search. In enclosing mode
// the start directory's own .vmake marker is not enough and a directory whose
// only buildscripts are children is not an enclosing project; the search looks
// for a real project root above the start directory instead.
func searchProjectDirFrom(start string, enclosing bool) (string, bool) {
	for current := start; ; {
		startIsProject := current == start && !enclosing
		if hasProjectConfig(current) && (startIsProject || hasProjectRootMarker(current)) {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	for current := start; ; {
		if hasManifest(current) {
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
	if !enclosing && hasChildBuildGo(start) {
		return start, true
	}
	return "", false
}

// findEnclosingProjectRoot locates the project root strictly above start. The
// start directory's own .vmake marker is not consulted, so a stale
// package-local marker cannot hide the project that contains it.
func findEnclosingProjectRoot(start string) (string, bool) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	abs = canonicalOrSelf(abs)
	parent := filepath.Dir(abs)
	if parent == abs {
		return "", false
	}
	return searchProjectDirFrom(parent, true)
}

func hasGitRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func hasProjectConfig(dir string) bool {
	marker := filepath.Join(dir, ".vmake")
	if filepath.Clean(marker) == filepath.Clean(vmakeDir) {
		return false
	}
	info, err := os.Stat(marker)
	if err != nil {
		return false
	}
	if globalInfo, err := os.Stat(vmakeDir); err == nil && os.SameFile(info, globalInfo) {
		return false
	}
	return true
}

func hasManifest(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, buildscript.ManifestFileName))
	return err == nil
}

func hasProjectRootMarker(dir string) bool {
	return hasManifest(dir) || hasBuildGo(dir)
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
// exactly one working tree under it (<repo>/<pkg>/src or local/<memberDir>/src).
func getDepsDir() string {
	return getDepsDirAt(findProjectDir())
}

func getDepsDirAt(root string) string {
	return filepath.Join(root, ".vmake_deps")
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
	return getLockfilePathAt(findProjectDir())
}

func getLockfilePathAt(root string) string {
	return filepath.Join(root, ".vmake", lockfile.LockfileName)
}
