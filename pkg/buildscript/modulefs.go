package buildscript

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// moduleFS is a read-only fs.FS that maps a virtual GOPATH prefix to a local
// module root. yaegi uses it for source imports; no symlinks or temporary
// directories are needed, so the same path works on Windows.
type moduleFS struct {
	prefix string
	root   string
}

func (m moduleFS) Open(name string) (fs.File, error) {
	name = filepath.Clean(name)
	prefix := filepath.Clean(m.prefix)
	if name == prefix {
		return os.Open(m.root)
	}
	if !strings.HasPrefix(name, prefix+string(os.PathSeparator)) {
		return nil, fs.ErrNotExist
	}
	rel := strings.TrimPrefix(name, prefix)
	rel = strings.TrimPrefix(rel, string(os.PathSeparator))
	if rel != "" && !filepath.IsLocal(rel) {
		return nil, fs.ErrNotExist
	}
	target := filepath.Join(m.root, rel)
	realRoot, err := filepath.EvalSymlinks(m.root)
	if err != nil {
		return nil, err
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, err
	}
	if !withinDir(realRoot, realTarget) {
		return nil, fs.ErrPermission
	}
	return os.Open(realTarget)
}
