package api

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
)

func CopyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}
	destFile, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer destFile.Close()

	destInfo, err := destFile.Stat()
	if err != nil {
		return err
	}
	if os.SameFile(srcInfo, destInfo) {
		return fmt.Errorf("copy file %s -> %s: source and destination are the same file", src, dest)
	}
	if err := destFile.Truncate(0); err != nil {
		return err
	}
	if _, err := io.Copy(destFile, srcFile); err != nil {
		return err
	}

	return destFile.Chmod(srcInfo.Mode())
}

type CopyFilter func(path string, isDir bool) bool

func CopyDir(src, dest string) error {
	return copyDirWithFilter(src, dest, nil)
}

func CopyDirWithFilter(src, dest string, filter CopyFilter) error {
	return copyDirWithFilter(src, dest, filter)
}

func copyDirWithFilter(src, dest string, filter CopyFilter) error {
	src = filepath.Clean(src)
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("copy directory %s: source is not a directory", src)
	}
	if err := fs.EnsureDir(dest); err != nil {
		return err
	}
	realSrc, err := fs.CanonicalPath(src)
	if err != nil {
		return err
	}
	realDest, err := fs.CanonicalPath(dest)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(realSrc, realDest); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("copy directory %s: destination %s is inside source", src, dest)
	}
	return fs.Walk(src, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil {
			if info != nil && info.Mode()&os.ModeSymlink != 0 && filter != nil && !filter(srcPath, info.IsDir()) {
				return nil
			}
			return err
		}
		if srcPath == src {
			return nil
		}
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if filter != nil && !filter(srcPath, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			realPath, err := fs.CanonicalPath(srcPath)
			if err != nil {
				return err
			}
			if rel, err := filepath.Rel(realDest, realPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("copy directory %s: source path %s reaches destination %s", src, srcPath, dest)
			}
		}
		rel, err := filepath.Rel(src, srcPath)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dest, rel)
		if info.IsDir() {
			return fs.EnsureDir(destPath)
		}
		return CopyFile(srcPath, destPath)
	})
}

func CopyDirIfExists(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil || !info.IsDir() {
		return nil
	}
	return CopyDir(src, dst)
}

func MatchPatterns(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(filepath.ToSlash(p), filepath.ToSlash(name)); ok {
			return true
		}
	}
	return false
}
