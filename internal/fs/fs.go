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

func IsStale(target string, sources []string) (bool, error) {
	targetInfo, err := os.Stat(target)
	if err != nil {
		return true, nil
	}
	targetTime := targetInfo.ModTime()
	for _, src := range sources {
		info, err := os.Stat(src)
		if err != nil {
			return true, nil
		}
		if info.ModTime().After(targetTime) {
			return true, nil
		}
	}
	return false, nil
}

func ListDirEntries(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			result = append(result, entry)
		}
	}
	return result, nil
}
