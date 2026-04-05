package build

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

func CopyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return err
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.Chmod(dest, srcInfo.Mode())
}

type CopyFilter func(path string, isDir bool) bool

func CopyDir(src, dest string) error {
	return copyDirWithFilter(src, dest, nil)
}

func CopyDirMatching(src, dest string, match func(string) bool) error {
	filter := func(path string, isDir bool) bool {
		if isDir {
			return true
		}
		return match(filepath.Base(path))
	}
	return copyDirWithFilter(src, dest, filter)
}

func CopyDirWithFilter(src, dest string, filter CopyFilter) error {
	return copyDirWithFilter(src, dest, filter)
}

func InstallFilter(path string, isDir bool) bool {
	if isDir {
		return true
	}
	return !strings.HasSuffix(path, ".go")
}

func copyDirWithFilter(src, dest string, filter CopyFilter) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		destPath := filepath.Join(dest, entry.Name())

		if entry.IsDir() {
			if entry.Name() == ".git" {
				continue
			}
			if filter != nil && !filter(srcPath, true) {
				continue
			}
			if err := copyDirWithFilter(srcPath, destPath, filter); err != nil {
				return err
			}
		} else {
			if filter != nil && !filter(srcPath, false) {
				continue
			}
			if err := CopyFile(srcPath, destPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func MatchPatterns(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}
