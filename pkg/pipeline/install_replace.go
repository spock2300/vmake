package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/storage"
	vlog "github.com/spock2300/vmake/pkg/log"
)

type installTxState string

const (
	installTransactionMarker = "vmake-install-transaction-v1\n"

	installTxExisting  installTxState = installTransactionMarker + "existing"
	installTxNew       installTxState = installTransactionMarker + "new"
	installTxCommitted installTxState = installTransactionMarker + "committed"
)

type installTransaction struct {
	prefix        string
	holding       string
	stagingParent string
	lock          string
	inProject     bool
}

func transactionForTarget(target, projectRoot string) installTransaction {
	if projectRoot != "" && InstallPathAncestor(projectRoot, target) {
		rel, err := filepath.Rel(projectRoot, target)
		key := "default"
		if err == nil && filepath.ToSlash(rel) != "install" {
			key = storage.OwnerKey(filepath.ToSlash(rel))[:16]
		}
		stagingParent := filepath.Join(projectRoot, ".vmake", "_install")
		return installTransaction{
			prefix:        target,
			holding:       filepath.Join(stagingParent, key),
			stagingParent: stagingParent,
			lock:          filepath.Join(projectRoot, ".vmake", "_locks", "install-"+key+".lock"),
			inProject:     true,
		}
	}
	parent := filepath.Dir(target)
	base := filepath.Base(target)
	return installTransaction{
		prefix:        target,
		holding:       filepath.Join(parent, "."+base+".vmake-installing"),
		stagingParent: parent,
		lock:          filepath.Join(parent, "."+base+".vmake-install.lock"),
	}
}

func pathPresent(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (tx installTransaction) recover() error {
	info, err := os.Lstat(tx.holding)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("install recovery path %s is not a directory", tx.holding)
	}
	statePath := filepath.Join(tx.holding, "state")
	info, err = os.Lstat(statePath)
	if err != nil {
		return fmt.Errorf("inspect install recovery record %s: %w", statePath, err)
	}
	if !info.Mode().IsRegular() || info.Size() > int64(len(installTxCommitted)) {
		return fmt.Errorf("unrecognized install recovery record %s", statePath)
	}
	marker, err := os.ReadFile(statePath)
	if err != nil {
		return fmt.Errorf("read install recovery record %s: %w", tx.holding, err)
	}
	previous := filepath.Join(tx.holding, "previous")
	switch installTxState(marker) {
	case installTxCommitted:
		if err := fs.RemoveAll(previous); err != nil {
			return err
		}
	case installTxExisting:
		if exists, err := pathPresent(previous); err != nil {
			return err
		} else if exists {
			if err := fs.RemoveAll(tx.prefix); err != nil {
				return err
			}
			if err := fs.RenameRetry(previous, tx.prefix); err != nil {
				return fmt.Errorf("restore installation from %s: %w", previous, err)
			}
		}
	case installTxNew:
		if err := fs.RemoveAll(tx.prefix); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unrecognized install recovery directory %s", tx.holding)
	}
	unused, err := os.MkdirTemp(tx.stagingParent, ".vmake-install-retired-*")
	if err != nil {
		return err
	}
	retired := filepath.Join(unused, "transaction")
	if err := fs.RenameRetry(tx.holding, retired); err != nil {
		return errors.Join(err, os.Remove(unused))
	}
	err = fs.RemoveAll(unused)
	if tx.inProject {
		_ = os.Remove(tx.stagingParent)
	}
	return err
}

func (tx installTransaction) begin() error {
	mode := installTxExisting
	if exists, err := pathPresent(tx.prefix); err != nil {
		return err
	} else if !exists {
		mode = installTxNew
	}
	if tx.inProject {
		if err := fs.EnsureDir(tx.stagingParent); err != nil {
			return err
		}
	}
	draft, err := os.MkdirTemp(tx.stagingParent, ".vmake-install-start-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(draft)
	if err := os.WriteFile(filepath.Join(draft, "state"), []byte(mode), 0600); err != nil {
		return err
	}
	if err := fs.RenameRetry(draft, tx.holding); err != nil {
		return err
	}
	if mode == installTxExisting {
		if err := fs.RenameRetry(tx.prefix, filepath.Join(tx.holding, "previous")); err != nil {
			return fmt.Errorf("preserve previous install %s: %w", tx.prefix, err)
		}
	}
	return fs.EnsureDir(tx.prefix)
}

func (tx installTransaction) commit() error {
	file := filepath.Join(tx.holding, "complete")
	if err := os.WriteFile(file, []byte(installTxCommitted), 0600); err != nil {
		return err
	}
	return fs.RenameRetry(file, filepath.Join(tx.holding, "state"))
}

func PrepareInstallPrefix(ctx context.Context, prefix, projectDir string, storageRoots ...string) (err error) {
	target, defaultInstall, projectRoot, err := validateInstallPrefixPath(prefix, projectDir, storageRoots...)
	if err != nil {
		return err
	}
	tx := transactionForTarget(target, projectRoot)
	hasHolding, err := pathPresent(tx.holding)
	if err != nil {
		return err
	}
	hasExternalLock := false
	if !tx.inProject {
		if hasExternalLock, err = pathPresent(tx.lock); err != nil {
			return err
		}
	}
	if !hasHolding && !hasExternalLock {
		return validateInstallPrefixOwnership(target, defaultInstall)
	}
	lock, err := flock.AcquireContext(ctx, tx.lock)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Release()) }()
	if err := tx.recover(); err != nil {
		return err
	}
	return validateInstallPrefixOwnership(target, defaultInstall)
}

func CleanInstallPrefix(projectDir string) {
	if projectDir == "" {
		return
	}
	projectRoot := CanonicalInstallPath(projectDir)
	if abs, err := filepath.Abs(projectDir); err == nil {
		projectRoot = CanonicalInstallPath(abs)
	}
	installPath := filepath.Join(projectDir, "install")
	if info, err := os.Lstat(installPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		target := CanonicalInstallPath(installPath)
		if target != installPath {
			tx := transactionForTarget(target, projectRoot)
			fs.RemoveIfExists(tx.holding)
		}
	}
	fs.RemoveIfExists(filepath.Join(projectDir, ".vmake", "_install"))
	RemoveIfExists(installPath, "", "install/", true)
}

func replaceInstallPrefix(ctx context.Context, prefix, projectDir string, storageRoots []string, install func() error) (err error) {
	target, defaultInstall, projectRoot, err := validateInstallPrefixPath(prefix, projectDir, storageRoots...)
	if err != nil {
		return err
	}
	tx := transactionForTarget(target, projectRoot)
	hasHolding, err := pathPresent(tx.holding)
	if err != nil {
		return err
	}
	hasLock, err := pathPresent(tx.lock)
	if err != nil {
		return err
	}
	if !hasHolding && !hasLock {
		if err := validateInstallPrefixOwnership(target, defaultInstall); err != nil {
			return err
		}
	}
	lock, err := flock.AcquireContext(ctx, tx.lock)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Release()) }()
	if err := tx.recover(); err != nil {
		return err
	}
	if err := validateInstallPrefixOwnership(target, defaultInstall); err != nil {
		return err
	}
	committed := false
	defer func() {
		if cleanupErr := tx.recover(); cleanupErr != nil {
			if committed {
				vlog.Error("Warning: installation published; recovery metadata at %s could not be cleaned: %v", tx.holding, cleanupErr)
			} else {
				err = errors.Join(err, cleanupErr)
			}
		}
	}()
	if err := tx.begin(); err != nil {
		return err
	}
	if err := install(); err != nil {
		return err
	}
	if err := tx.commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
