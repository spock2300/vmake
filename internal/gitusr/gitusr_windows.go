//go:build windows

// Package gitusr locates the Unix userland bundled with Git for Windows and
// makes it resolvable by name. The default installer only puts <root>\cmd on
// PATH, so sh, coreutils, tar, unzip and curl are present on disk but
// invisible to exec.LookPath.
package gitusr

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/gitcmd"
)

// SetupProcessEnv prepends the discovered Git for Windows directories to this
// process' PATH and returns the directories that were added. It must be called
// before any goroutine starts, because os.Setenv is not safe for concurrent use.
func SetupProcessEnv() []string {
	root, msystem, ok := findLayout()
	if !ok {
		return nil
	}

	// vmake's layout is symlink-based, so coreutils 'ln -s' must create real
	// symlinks rather than silently copying or emitting .lnk shortcuts.
	if os.Getenv("MSYS") == "" {
		_ = os.Setenv("MSYS", "winsymlinks:nativestrict")
	}
	return prependPath(userlandDirs(root, msystem))
}

// UsrBin returns the Git for Windows usr/bin directory when it exists.
func UsrBin() (string, bool) {
	root, _, ok := findLayout()
	if !ok {
		return "", false
	}
	dir := path.Join(root, "usr", "bin")
	if !isDir(dir) {
		return "", false
	}
	return filepath.FromSlash(dir), true
}

func findLayout() (root, msystem string, ok bool) {
	if gitPath, err := exec.LookPath("git"); err == nil {
		if r, m, ok := layoutFromGitPath(gitPath); ok {
			return r, m, true
		}
	}
	out, err := exec.Command("git", gitcmd.Args("--exec-path")...).Output()
	if err != nil {
		return "", "", false
	}
	return layoutFromExecPath(strings.TrimSpace(string(out)))
}

func prependPath(dirs []string) []string {
	if len(dirs) == 0 {
		return nil
	}
	sep := string(os.PathListSeparator)
	cur := os.Getenv("PATH")

	present := make(map[string]bool)
	for _, p := range filepath.SplitList(cur) {
		if p != "" {
			present[strings.ToLower(filepath.Clean(p))] = true
		}
	}

	var added []string
	for _, d := range dirs {
		key := strings.ToLower(filepath.Clean(d))
		if present[key] {
			continue
		}
		present[key] = true
		added = append(added, d)
	}
	if len(added) == 0 {
		return nil
	}

	joined := strings.Join(added, sep)
	if cur != "" {
		joined += sep + cur
	}
	_ = os.Setenv("PATH", joined)
	return added
}
