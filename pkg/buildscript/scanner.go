package buildscript

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/gosrc"
	"github.com/spock2300/vmake/pkg/api"
)

var skipDirs = map[string]bool{
	".git":         true,
	".vmake":       true,
	"build":        true,
	"vendor":       true,
	"node_modules": true,
	"vmake_deps":   true,
}

func Scan(rootDir string) ([]Source, error) {
	var sources []Source
	seen := make(map[string]bool)
	namesSeen := make(map[string]bool)

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			name := info.Name()
			if skipDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if info.Name() == "build.go" {
			dir := filepath.Dir(path)
			pkgName := filepath.Base(dir)

			if seen[dir] || namesSeen[pkgName] {
				return nil
			}
			seen[dir] = true
			namesSeen[pkgName] = true

			sources = append(sources, Source{
				Name:   pkgName,
				Path:   path,
				Dir:    dir,
				Origin: api.SourceLocal,
			})
		}

		return nil
	})

	return sources, err
}

func ListGoFiles(dir string) ([]string, error) {
	return gosrc.ListGoFiles(dir)
}

// ScriptSetHash summarizes a package's buildscript files (sorted paths +
// contents). It feeds the BuildKey so script edits or deletions rotate build
// directories instead of silently reusing stale outputs.
func ScriptSetHash(dir string) (string, error) {
	files, err := gosrc.ListGoFiles(dir)
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00", f)
		h.Write(data)
		h.Write([]byte("\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

func ScanSubPackages(rootDir string, parentID string) ([]Source, error) {
	var sources []Source
	seen := make(map[string]bool)

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			name := info.Name()
			if skipDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if info.Name() == "build.go" {
			dir := filepath.Dir(path)
			if dir == rootDir {
				return nil
			}

			relDir, err := filepath.Rel(rootDir, dir)
			if err != nil || strings.HasPrefix(relDir, "..") {
				return nil
			}

			qualifiedName := parentID + "/" + filepath.ToSlash(relDir)

			if seen[dir] {
				return nil
			}
			seen[dir] = true

			sources = append(sources, Source{
				Name:   qualifiedName,
				Path:   path,
				Dir:    dir,
				Origin: api.SourceRemote,
			})
		}

		return nil
	})

	return sources, err
}
