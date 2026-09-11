package gitusr

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// knownMsystems lists the MSYS2 environments a Git for Windows installation can
// use, most-preferred first.
var knownMsystems = []string{"ucrt64", "mingw64", "mingw32", "clangarm64", "clang64"}

// layoutFromExecPath derives the installation root and MSYS environment from
// the output of `git --exec-path`, e.g.
// "C:\Program Files\Git\mingw64\libexec\git-core".
func layoutFromExecPath(execPath string) (root, msystem string, ok bool) {
	core := toSlash(execPath)
	if core == "" || !isDir(core) {
		return "", "", false
	}

	msysDir := path.Dir(path.Dir(core))
	msystem = strings.ToLower(path.Base(msysDir))
	if !isKnownMsystem(msystem) {
		return "", "", false
	}

	root = path.Dir(msysDir)
	if !isDir(path.Join(root, "usr", "bin")) {
		return "", "", false
	}
	return root, msystem, true
}

// layoutFromGitPath derives the installation root from the location of the
// git executable, e.g. "C:\Program Files\Git\cmd\git.exe" or
// "C:\Program Files\Git\mingw64\bin\git.exe".
func layoutFromGitPath(gitPath string) (root, msystem string, ok bool) {
	gitPath = toSlash(gitPath)
	if gitPath == "" {
		return "", "", false
	}
	binDir := path.Dir(gitPath)
	base := strings.ToLower(path.Base(binDir))
	if base != "cmd" && base != "bin" {
		return "", "", false
	}

	// <root>\<msystem>\bin\git.exe
	parent := path.Dir(binDir)
	if m := strings.ToLower(path.Base(parent)); isKnownMsystem(m) {
		root = path.Dir(parent)
		if isDir(path.Join(root, "usr", "bin")) {
			return root, m, true
		}
	}

	// <root>\cmd\git.exe or <root>\bin\git.exe
	root = parent
	if isDir(path.Join(root, "usr", "bin")) {
		return root, detectMsystem(root), true
	}
	return "", "", false
}

func detectMsystem(root string) string {
	for _, m := range knownMsystems {
		if isDir(path.Join(root, m, "bin")) {
			return m
		}
	}
	return ""
}

// userlandDirs returns the existing directories holding Git for Windows'
// bundled Unix tools, in PATH precedence order: the MSYS userland first, then
// the MinGW toolchain binaries.
func userlandDirs(root, msystem string) []string {
	var dirs []string
	if p := path.Join(root, "usr", "bin"); isDir(p) {
		dirs = append(dirs, filepath.FromSlash(p))
	}
	if msystem != "" {
		if p := path.Join(root, msystem, "bin"); isDir(p) {
			dirs = append(dirs, filepath.FromSlash(p))
		}
	}
	return dirs
}

func isKnownMsystem(name string) bool {
	for _, m := range knownMsystems {
		if m == name {
			return true
		}
	}
	return false
}

// toSlash normalizes a Windows path to forward slashes so that path (not
// filepath) can be used for derivation on any host OS.
func toSlash(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}

func isDir(p string) bool {
	info, err := os.Stat(filepath.FromSlash(p))
	return err == nil && info.IsDir()
}
