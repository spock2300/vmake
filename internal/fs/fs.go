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

func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func IsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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

func HasFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

func DetectLibDir(installDir string) string {
	lib64Dir := filepath.Join(installDir, "lib64")
	if FileExists(lib64Dir) {
		return lib64Dir
	}
	return filepath.Join(installDir, "lib")
}
