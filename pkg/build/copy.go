package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

// FileHash returns the SHA256 of a file's content, used for exact
// publish-skip decisions where size+mtime would accept divergent content.
func FileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func CopyFile(src, dest string) error {
	if copyFileUnchanged(src, dest) {
		return nil
	}
	return api.CopyFile(src, dest)
}

func copyFileUnchanged(src, dest string) bool {
	srcInfo, err := os.Stat(src)
	if err != nil || !srcInfo.Mode().IsRegular() {
		return false
	}
	destInfo, err := os.Stat(dest)
	if err != nil || !destInfo.Mode().IsRegular() || os.SameFile(srcInfo, destInfo) || srcInfo.Size() != destInfo.Size() || srcInfo.Mode() != destInfo.Mode() {
		return false
	}
	srcHash, err := FileHash(src)
	if err != nil {
		return false
	}
	destHash, err := FileHash(dest)
	return err == nil && srcHash == destHash
}

type CopyFilter = api.CopyFilter

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

func copyDirWithFilter(src, dest string, filter CopyFilter) error {
	var relativeErr error
	err := api.CopyDirWithFilter(src, dest, func(path string, isDir bool) bool {
		if filter != nil && !filter(path, isDir) {
			return false
		}
		if isDir {
			return true
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			relativeErr = err
			return false
		}
		return !copyFileUnchanged(path, filepath.Join(dest, rel))
	})
	if err != nil {
		return err
	}
	return relativeErr
}

func MatchPatterns(patterns []string, name string) bool {
	return api.MatchPatterns(patterns, name)
}
