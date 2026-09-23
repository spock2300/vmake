package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/internal/storage"
)

type sourceRef struct {
	Commit string `json:"commit"`
}

func (m *SourceManager) EnsureURLRef(url, ref, expectedCommit string, refresh bool) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("source ref must not be empty")
	}
	if expectedCommit != "" && !validCommitID(expectedCommit) {
		return "", fmt.Errorf("invalid source commit %q", expectedCommit)
	}
	release, err := m.acquireAccess(false)
	if err != nil {
		return "", err
	}
	defer release()
	urlKey := storage.OwnerKey(url)
	dir := filepath.Join(storage.CacheDir(m.globalDir), "_localgit", urlKey)
	lock, err := flock.AcquireContext(m.context(), filepath.Join(m.locksDir, "localgit_"+urlKey[:16]+".lock"))
	if err != nil {
		return "", err
	}
	defer lock.Release()
	if expectedCommit != "" {
		seed := filepath.Join(dir, "commits", expectedCommit, "src")
		if fs.FileExists(filepath.Join(seed, ".git")) {
			return checkedSourceCommit(m.context(), seed, expectedCommit)
		}
	}
	refFile := filepath.Join(dir, "refs", storage.OwnerKey(ref)+".json")
	if !refresh {
		var selected sourceRef
		if err := jsonio.Load(refFile, &selected); err == nil {
			if !validCommitID(selected.Commit) {
				return "", fmt.Errorf("cached source ref %s has invalid commit %q", ref, selected.Commit)
			}
			if expectedCommit != "" && selected.Commit != expectedCommit {
				return "", fmt.Errorf("cached source ref %s is commit %s but expected %s", ref, selected.Commit, expectedCommit)
			}
			seed := filepath.Join(dir, "commits", selected.Commit, "src")
			if fs.FileExists(filepath.Join(seed, ".git")) {
				return checkedSourceCommit(m.context(), seed, selected.Commit)
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	if err := fs.EnsureDir(dir); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(dir, ".ref-")
	if err != nil {
		return "", err
	}
	defer fs.RemoveIfExists(tmp)
	urlSource := filepath.Join(dir, "src")
	if fs.FileExists(filepath.Join(urlSource, ".git")) {
		if err := cloneRepo(m.context(), urlSource, tmp, []string{"--no-checkout"}); err != nil {
			return "", err
		}
		if err := gitRunContext(m.context(), tmp, []string{"remote", "set-url", "origin", url}, 0); err != nil {
			return "", err
		}
	} else {
		if err := gitRunContext(m.context(), tmp, []string{"init", "-q"}, 0); err != nil {
			return "", err
		}
		if err := gitRunContext(m.context(), tmp, []string{"remote", "add", "origin", url}, 0); err != nil {
			return "", err
		}
	}
	if err := gitRunContext(m.context(), tmp, []string{"fetch", "--depth", "1", "--no-tags", "origin", ref}, 5*time.Minute); err != nil {
		return "", err
	}
	if err := CheckoutContext(m.context(), tmp, "FETCH_HEAD"); err != nil {
		return "", err
	}
	commit, err := GetCurrentCommitContext(m.context(), tmp)
	if err != nil {
		return "", err
	}
	if expectedCommit != "" && commit != expectedCommit {
		return "", fmt.Errorf("source ref %s resolves to commit %s but expected %s", ref, commit, expectedCommit)
	}
	if err := m.context().Err(); err != nil {
		return "", err
	}
	seed := filepath.Join(dir, "commits", commit, "src")
	if fs.FileExists(filepath.Join(seed, ".git")) {
		if _, err := checkedSourceCommit(m.context(), seed, commit); err != nil {
			return "", err
		}
	} else {
		if err := fs.EnsureParentDir(seed); err != nil {
			return "", err
		}
		if err := fs.RenameRetry(tmp, seed); err != nil {
			return "", err
		}
	}
	if err := m.context().Err(); err != nil {
		return "", err
	}
	if err := jsonio.Save(refFile, sourceRef{Commit: commit}); err != nil {
		return "", err
	}
	return seed, nil
}

func checkedSourceCommit(ctx context.Context, seed, expected string) (string, error) {
	commit, err := GetCurrentCommitContext(ctx, seed)
	if err != nil {
		return "", err
	}
	if commit != expected {
		return "", fmt.Errorf("cached source %s is commit %s but expected %s", seed, commit, expected)
	}
	return seed, nil
}

func validCommitID(commit string) bool {
	return (len(commit) == 40 || len(commit) == 64) && strings.Trim(commit, "0123456789abcdef") == ""
}
