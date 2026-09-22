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
	return api.CopyFile(src, dest)
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
	return api.CopyDirWithFilter(src, dest, filter)
}

func MatchPatterns(patterns []string, name string) bool {
	return api.MatchPatterns(patterns, name)
}
