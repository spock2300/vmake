package fs

import (
	"fmt"
	"os"
	"path/filepath"
)

func Walk(root string, fn filepath.WalkFunc) error {
	err := walk(filepath.Clean(root), nil, fn)
	if err == filepath.SkipDir || err == filepath.SkipAll {
		return nil
	}
	return err
}

func walk(path string, ancestors []os.FileInfo, fn filepath.WalkFunc) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fn(path, nil, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		linkInfo := info
		info, err = os.Stat(path)
		if err != nil {
			return fn(path, linkInfo, err)
		}
	}
	if err := fn(path, info, nil); err != nil {
		if err == filepath.SkipDir && info.IsDir() {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	for _, ancestor := range ancestors {
		if os.SameFile(info, ancestor) {
			return fn(path, info, fmt.Errorf("directory symlink cycle at %s", path))
		}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fn(path, info, err)
	}
	ancestors = append(ancestors, info)
	for _, entry := range entries {
		if err := walk(filepath.Join(path, entry.Name()), ancestors, fn); err != nil {
			if err == filepath.SkipDir {
				return nil
			}
			return err
		}
	}
	return nil
}

func CanonicalPath(path string) (string, error) {
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(realPath)
}
