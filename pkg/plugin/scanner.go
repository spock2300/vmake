package plugin

import (
	"os"
	"path/filepath"
	"strings"
)

type Package struct {
	Name string
	Path string
	Dir  string
}

var skipDirs = map[string]bool{
	".git":         true,
	".vmake":       true,
	"build":        true,
	"vendor":       true,
	"node_modules": true,
}

func Scan(rootDir string) ([]Package, error) {
	var packages []Package
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

			packages = append(packages, Package{
				Name: pkgName,
				Path: path,
				Dir:  dir,
			})
		}

		return nil
	})

	return packages, err
}
