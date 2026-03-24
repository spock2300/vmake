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

func CopyDir(src, dest string) error {
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
			if err := CopyDir(srcPath, destPath); err != nil {
				return err
			}
		} else {
			if strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			if err := CopyFile(srcPath, destPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func CopyDirMatching(src, dest string, match func(string) bool) error {
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
			if err := CopyDirMatching(srcPath, destPath, match); err != nil {
				return err
			}
		} else {
			if strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			if match(entry.Name()) {
				if err := CopyFile(srcPath, destPath); err != nil {
					return err
				}
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
