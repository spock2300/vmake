package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/storage"
)

func (m *SourceManager) WithContext(ctx context.Context) *SourceManager {
	m.ctx = ctx
	return m
}

func (m *SourceManager) context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *SourceManager) WithSession(session *storage.Session) *SourceManager {
	m.session = session
	return m
}

func (m *SourceManager) acquireAccess(exclusive bool) (func(), error) {
	if err := m.context().Err(); err != nil {
		return nil, err
	}
	if m.session != nil {
		if err := m.session.CheckAccess(m.globalDir, exclusive); err != nil {
			return nil, err
		}
		return func() {}, nil
	}
	session, err := storage.AcquireContext(m.context(), "", m.globalDir, exclusive)
	if err != nil {
		return nil, err
	}
	return func() { _ = session.Close() }, nil
}

func (m *SourceManager) CleanOutputs(repoName, name string) error {
	release, err := m.acquireAccess(true)
	if err != nil {
		return err
	}
	defer release()
	ownerDir := filepath.Join(storage.CacheDir(m.globalDir), repoName, name)
	entries, err := os.ReadDir(ownerDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		if err := fs.RemoveAll(filepath.Join(ownerDir, entry.Name(), "out")); err != nil {
			return err
		}
	}
	return nil
}

func (m *SourceManager) ProjectVersion(repoName, name string) (string, error) {
	link := filepath.Join(m.sourcesDir, repoName, name, "src")
	target, err := filepath.EvalSymlinks(link)
	if err != nil {
		return "", err
	}
	ownerDir := canonicalPath(filepath.Join(storage.CacheDir(m.globalDir), repoName, name))
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(ownerDir, target)
	if err != nil {
		return "", err
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 || parts[0] == ".." || parts[0] == "." || parts[1] != "src" || strings.HasPrefix(parts[0], "_") {
		return "", fmt.Errorf("source link %s is not a managed version checkout", link)
	}
	return parts[0], nil
}
