package fs

import (
	"fmt"
	"os"
	"path/filepath"
)

func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	return nil
}

func EnsureParentDir(filePath string) error {
	return EnsureDir(filepath.Dir(filePath))
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func RemoveAll(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func RemoveIfExists(path string) {
	_ = os.RemoveAll(path)
}

func ListDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func DetectLibDir(installDir string) string {
	lib64Dir := filepath.Join(installDir, "lib64")
	if FileExists(lib64Dir) {
		return lib64Dir
	}
	return filepath.Join(installDir, "lib")
}
