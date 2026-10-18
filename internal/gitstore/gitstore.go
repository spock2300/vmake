package gitstore

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
)

type CloneFunc func(gitURL, dest string) error

type Store struct {
	baseDir string
}

func New(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

func (s *Store) BaseDir() string { return s.baseDir }

// ValidateName rejects names that are not a single filesystem component.
// Repository names come from CLI arguments and are joined below the store
// root; accepting separators or ".." would let a command remove arbitrary
// directories.
func ValidateName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid name %q", name)
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("invalid name %q: leading or trailing whitespace", name)
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("invalid name %q: whitespace is not allowed", name)
	}
	if strings.ContainsAny(name, `/\:@`) {
		return fmt.Errorf("invalid name %q: path separators, ':' and '@' are not allowed", name)
	}
	if strings.HasSuffix(name, ".") {
		return fmt.Errorf("invalid name %q: trailing dot", name)
	}
	base := strings.ToUpper(name)
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return fmt.Errorf("invalid name %q: reserved on Windows", name)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("invalid name %q: NUL is not allowed", name)
	}
	if !filepath.IsLocal(name) {
		return fmt.Errorf("invalid name %q", name)
	}
	return nil
}

func (s *Store) Path(name string) string {
	if err := ValidateName(name); err != nil {
		return ""
	}
	return filepath.Join(s.baseDir, name)
}

func (s *Store) Exists(name string) bool {
	path := s.Path(name)
	return path != "" && fs.FileExists(path)
}

func (s *Store) Add(name, gitURL string, clone CloneFunc) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	repoPath := s.Path(name)
	if fs.FileExists(repoPath) {
		return fmt.Errorf("'%s' already exists", name)
	}
	if err := fs.EnsureParentDir(repoPath); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	if err := clone(gitURL, repoPath); err != nil {
		return fmt.Errorf("failed to clone: %w", err)
	}
	return nil
}

func (s *Store) Remove(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	repoPath := s.Path(name)
	if !fs.FileExists(repoPath) {
		return fmt.Errorf("'%s' not found", name)
	}
	return fs.RemoveAll(repoPath)
}

func (s *Store) List() ([]string, error) {
	names, err := fs.ListDirs(s.baseDir)
	if err != nil {
		return nil, err
	}
	valid := names[:0]
	for _, name := range names {
		if ValidateName(name) == nil {
			valid = append(valid, name)
		}
	}
	return valid, nil
}
