package plugin

import (
	"os"
	"path/filepath"
	"strings"
)

var skipDirs = map[string]bool{
	".git":         true,
	".vmake":       true,
	"build":        true,
	"vendor":       true,
	"node_modules": true,
}

func Scan(rootDir string) ([]Source, error) {
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
			pkgName := filepath.Base(dir)

			if seen[pkgName] {
				return nil
			}
			seen[pkgName] = true

			sources = append(sources, Source{
				Name:   pkgName,
				Path:   path,
				Dir:    dir,
				Origin: SourceLocal,
			})
		}

		return nil
	})

	return sources, err
}
