package gitstore

import (
	"fmt"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
)

type CloneFunc func(gitURL, dest string) error

type Store struct {
	baseDir string
}

func New(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

func (s *Store) BaseDir() string { return s.baseDir }

func (s *Store) Path(name string) string {
	return filepath.Join(s.baseDir, name)
}

func (s *Store) Exists(name string) bool {
	return fs.FileExists(s.Path(name))
}

func (s *Store) Add(name, gitURL string, clone CloneFunc) error {
	repoPath := s.Path(name)
	if s.Exists(name) {
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
	repoPath := s.Path(name)
	if !s.Exists(name) {
		return fmt.Errorf("'%s' not found", name)
	}
	return fs.RemoveAll(repoPath)
}

func (s *Store) List() ([]string, error) {
	return fs.ListDirs(s.baseDir)
}
