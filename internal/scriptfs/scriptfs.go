// Package scriptfs gives interpreted build.go/plugin code script-relative
// file IO semantics. The process working directory stays untouched — instead
// the os/os-exec/filepath symbols an interpreter sees are wrapped so relative
// paths resolve against the script's own directory. This is safe under
// vmake's package-parallel scheduler, where a process-wide chdir would race.
package scriptfs

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"

	"github.com/traefik/yaegi/interp"
)

type ScriptFS struct {
	base     string
	baseFunc func() string
}

func New(base string) *ScriptFS {
	return &ScriptFS{base: base}
}

// Base returns the directory relative paths resolve against.
func (s *ScriptFS) Base() string {
	if s.baseFunc != nil {
		return s.baseFunc()
	}
	return s.base
}

func (s *ScriptFS) SetBaseFunc(fn func() string) {
	s.baseFunc = fn
}

func (s *ScriptFS) resolve(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(s.Base(), p)
}

// Exports returns yaegi symbol overrides for "os", "os/exec" and
// "path/filepath". A later i.Use of these entries replaces the stdlib
// bindings (interp.Use merges by import path + symbol name).
func (s *ScriptFS) Exports() interp.Exports {
	return interp.Exports{
		"os/os": {
			"Open":       reflect.ValueOf(s.open),
			"OpenFile":   reflect.ValueOf(s.openFile),
			"Create":     reflect.ValueOf(s.create),
			"ReadFile":   reflect.ValueOf(s.readFile),
			"WriteFile":  reflect.ValueOf(s.writeFile),
			"Stat":       reflect.ValueOf(s.stat),
			"Lstat":      reflect.ValueOf(s.lstat),
			"Mkdir":      reflect.ValueOf(s.mkdir),
			"MkdirAll":   reflect.ValueOf(s.mkdirAll),
			"Remove":     reflect.ValueOf(s.remove),
			"RemoveAll":  reflect.ValueOf(s.removeAll),
			"Rename":     reflect.ValueOf(s.rename),
			"ReadDir":    reflect.ValueOf(s.readDir),
			"Getwd":      reflect.ValueOf(s.getwd),
			"Chdir":      reflect.ValueOf(s.chdir),
			"TempDir":    reflect.ValueOf(s.tempDir),
			"CreateTemp": reflect.ValueOf(s.createTemp),
		},
		"os/exec/exec": {
			"Command": reflect.ValueOf(s.command),
		},
		"path/filepath/filepath": {
			"Walk":    reflect.ValueOf(s.walk),
			"WalkDir": reflect.ValueOf(s.walkDir),
		},
	}
}

func (s *ScriptFS) open(name string) (*os.File, error) { return os.Open(s.resolve(name)) }
func (s *ScriptFS) openFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(s.resolve(name), flag, perm)
}
func (s *ScriptFS) create(name string) (*os.File, error) { return os.Create(s.resolve(name)) }
func (s *ScriptFS) readFile(name string) ([]byte, error) { return os.ReadFile(s.resolve(name)) }
func (s *ScriptFS) writeFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(s.resolve(name), data, perm)
}
func (s *ScriptFS) stat(name string) (os.FileInfo, error)     { return os.Stat(s.resolve(name)) }
func (s *ScriptFS) lstat(name string) (os.FileInfo, error)    { return os.Lstat(s.resolve(name)) }
func (s *ScriptFS) mkdir(name string, perm os.FileMode) error { return os.Mkdir(s.resolve(name), perm) }
func (s *ScriptFS) mkdirAll(name string, perm os.FileMode) error {
	return os.MkdirAll(s.resolve(name), perm)
}
func (s *ScriptFS) remove(name string) error    { return os.Remove(s.resolve(name)) }
func (s *ScriptFS) removeAll(path string) error { return os.RemoveAll(s.resolve(path)) }
func (s *ScriptFS) rename(oldpath, newpath string) error {
	return os.Rename(s.resolve(oldpath), s.resolve(newpath))
}
func (s *ScriptFS) readDir(name string) ([]os.DirEntry, error) { return os.ReadDir(s.resolve(name)) }

func (s *ScriptFS) getwd() (string, error) { return s.Base(), nil }

func (s *ScriptFS) chdir(string) error {
	return fmt.Errorf("os.Chdir is not allowed in interpreted scripts (breaks parallel builds); use absolute paths or p.RunIn(dir, ...)")
}

func (s *ScriptFS) tempDir() string { return os.TempDir() }

func (s *ScriptFS) createTemp(dir, pattern string) (*os.File, error) {
	return os.CreateTemp(s.resolve(dir), pattern)
}

func (s *ScriptFS) command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	if cmd.Dir == "" {
		cmd.Dir = s.Base()
	}
	return cmd
}

func (s *ScriptFS) walk(root string, fn filepath.WalkFunc) error {
	return filepath.Walk(s.resolve(root), fn)
}

func (s *ScriptFS) walkDir(root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(s.resolve(root), fn)
}
