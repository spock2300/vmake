package toolchain

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spock2300/vmake/internal/assets"
	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/gitcmd"
)

func Install(def ToolchainDef, repoDir, toolchainsDir string) (*Toolchain, error) {
	if err := def.Validate(); err != nil {
		return nil, err
	}
	installCfg, err := def.Installation()
	if err != nil {
		return nil, err
	}
	if installCfg == nil {
		return nil, fmt.Errorf("toolchain %s has no installation", def.Name)
	}
	installDir := def.InstallDir(toolchainsDir)
	lock, err := flock.Acquire(filepath.Join(toolchainsDir, "_locks", runtime.GOOS, runtime.GOARCH, def.Name, def.Version+".lock"))
	if err != nil {
		return nil, fmt.Errorf("lock toolchain %s: %w", def.Name, err)
	}
	defer lock.Release()
	tc, err := def.ToToolchain(toolchainsDir)
	if err != nil {
		return nil, err
	}
	if tc.InstallPath != "" {
		return validatedToolchain(tc)
	}
	if err := os.MkdirAll(filepath.Dir(installDir), 0755); err != nil {
		return nil, fmt.Errorf("create toolchain parent: %w", err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(installDir), ".install-")
	if err != nil {
		return nil, fmt.Errorf("create toolchain staging directory: %w", err)
	}
	defer os.RemoveAll(stage)

	fmt.Printf("Installing %s for %s/%s...\n", def.Name, runtime.GOOS, runtime.GOARCH)
	var archivePath string
	switch installCfg.Method {
	case "lfs":
		archivePath = filepath.Join(repoDir, "assets", "toolchains", installCfg.File)
		if err := materializeToolchainAsset(repoDir, archivePath, installCfg.File); err != nil {
			return nil, err
		}
	case "http":
		archivePath = filepath.Join(stage, installCfg.File)
		if err := assets.DownloadFile(installCfg.URL, archivePath); err != nil {
			return nil, fmt.Errorf("failed to download: %w", err)
		}
	}
	if err := verifyToolchainArchive(archivePath, installCfg.Sha256); err != nil {
		return nil, err
	}
	extractDir := filepath.Join(stage, "extracted")
	if err := assets.ExtractToDir(archivePath, extractDir, installCfg.Format); err != nil {
		return nil, fmt.Errorf("extract toolchain %s: %w", def.Name, err)
	}
	root := filepath.Join(extractDir, installCfg.RootDir)
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("toolchain %s root_dir %s: %w", def.Name, installCfg.RootDir, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("toolchain %s root_dir %s is not a directory", def.Name, installCfg.RootDir)
	}
	tc.InstallPath = root
	if errs := ValidateToolchain(tc); len(errs) > 0 {
		return nil, fmt.Errorf("toolchain %s extracted tools: %w", def.Name, errors.Join(errs...))
	}
	if err := os.Rename(root, installDir); err != nil {
		return nil, fmt.Errorf("publish toolchain %s: %w", def.Name, err)
	}
	tc.InstallPath = installDir
	fmt.Printf("Toolchain %s installed to %s\n", def.Name, installDir)
	return tc, nil
}

func materializeToolchainAsset(repoDir, archivePath, file string) error {
	pointer, err := isToolchainAssetPointer(archivePath)
	if err == nil {
		if !pointer {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read toolchain archive: %w", err)
	}
	args := gitcmd.Args("-c", "lfs.fetchrecentalways=false", "lfs", "pull", "--include", "assets/toolchains/"+file, "--exclude", "")
	if err := iexec.RunToStdout(repoDir, "git", args...); err != nil {
		return fmt.Errorf("download toolchain archive: %w", err)
	}
	pointer, err = isToolchainAssetPointer(archivePath)
	if err != nil {
		return fmt.Errorf("materialize toolchain archive: %w", err)
	}
	if pointer {
		return fmt.Errorf("toolchain archive %s remains a Git LFS pointer after download", archivePath)
	}
	return nil
}

func isToolchainAssetPointer(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	header := make([]byte, 128)
	n, err := f.Read(header)
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.HasPrefix(string(header[:n]), "version https://git-lfs.github.com/spec/v1"), nil
}

func verifyToolchainArchive(path, expected string) error {
	if expected == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open toolchain archive: %w", err)
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return fmt.Errorf("hash toolchain archive: %w", err)
	}
	actual := fmt.Sprintf("%x", hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("toolchain archive %s: SHA256 mismatch: got %s, expected %s", path, actual, expected)
	}
	return nil
}
