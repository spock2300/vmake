package repo

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/flock"
	internalfs "github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/storage"
)

func (m *SourceManager) EnsureWorkspace(seedDir, workDir, identity string) error {
	release, err := m.acquireAccess(false)
	if err != nil {
		return err
	}
	defer release()
	return m.ensureWorkspace(seedDir, workDir, identity)
}

func (m *SourceManager) ensureWorkspace(seedDir, workDir, identity string) error {
	seedDir, err := filepath.EvalSymlinks(seedDir)
	if err != nil {
		return err
	}
	workDir = canonicalPath(workDir)
	lock, err := flock.AcquireContext(m.context(), filepath.Join(m.locksDir, "workspace_"+storage.OwnerKey(workDir)+".lock"))
	if err != nil {
		return err
	}
	defer lock.Release()
	identity = seedDir + "\x00" + identity
	marker := workDir + ".vmake-workspace"
	if data, err := os.ReadFile(marker); err == nil && string(data) == identity {
		if info, err := os.Stat(workDir); err == nil && info.IsDir() {
			return nil
		}
	}
	if err := internalfs.EnsureParentDir(workDir); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(workDir), ".workspace-")
	if err != nil {
		return err
	}
	defer internalfs.RemoveIfExists(tmp)
	if err := copyWorkspaceTreeContext(m.context(), seedDir, tmp); err != nil {
		return fmt.Errorf("copy source workspace: %w", err)
	}
	if err := m.context().Err(); err != nil {
		return err
	}
	markerTmp := tmp + ".vmake-workspace"
	defer internalfs.RemoveIfExists(markerTmp)
	if err := os.WriteFile(markerTmp, []byte(identity), 0644); err != nil {
		return err
	}
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := internalfs.RemoveAll(workDir); err != nil {
		return err
	}
	if err := internalfs.RenameRetry(tmp, workDir); err != nil {
		return err
	}
	return internalfs.RenameRetry(markerTmp, marker)
}

// canonicalPath resolves symlinks in the deepest existing ancestor of path,
// leaving trailing components that do not exist yet unchanged, so that
// distinct spellings of the same directory share one lock and marker identity.
func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return resolveExistingPath(abs)
}

func resolveExistingPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(resolveExistingPath(parent), filepath.Base(path))
}

func copyWorkspaceTree(src, dest string) error {
	return copyWorkspaceTreeContext(context.Background(), src, dest)
}

func copyWorkspaceTreeContext(ctx context.Context, src, dest string) error {
	if err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil || !entry.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.MkdirAll(filepath.Join(dest, rel), info.Mode().Perm()|0700)
	}); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported source file %s (%s)", path, info.Mode())
		}
		return copyWorkspaceFile(ctx, path, target, info)
	})
}

func copyWorkspaceFile(ctx context.Context, src, dest string, info fs.FileInfo) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()|0200)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, &workspaceReader{ctx: ctx, input: input})
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chtimes(dest, info.ModTime(), info.ModTime())
}

type workspaceReader struct {
	ctx   context.Context
	input *os.File
}

func (r *workspaceReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.input.Read(p)
}
