package plugin

import (
	"fmt"
	"path/filepath"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
)

func RunGitLFS(repoDir string, args ...string) error {
	fullArgs := append([]string{"lfs"}, args...)
	if err := iexec.RunToStdout(repoDir, "git", fullArgs...); err != nil {
		return fmt.Errorf("git lfs failed: %w", err)
	}
	return nil
}

func DownloadFile(url, dest string) error {
	if err := fs.EnsureParentDir(dest); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := iexec.RunToStdout("", "curl", "-L", "-o", dest, url); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}

func ExtractToDir(archive, dest, format string) error {
	if err := fs.EnsureDir(dest); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if format == "" {
		format = detectFormat(archive)
	}

	switch format {
	case "tar.gz", "tgz":
		return runExtract("tar", "-xzf", archive, "-C", dest)
	case "tar.xz", "txz":
		return runExtract("tar", "-xJf", archive, "-C", dest)
	case "tar.bz2", "tbz2":
		return runExtract("tar", "-xjf", archive, "-C", dest)
	case "zip":
		return runExtract("unzip", "-o", archive, "-d", dest)
	default:
		return runExtract("tar", "-xzf", archive, "-C", dest)
	}
}

func runExtract(name string, args ...string) error {
	if err := iexec.RunToStdout("", name, args...); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}
	return nil
}

func detectFormat(filename string) string {
	base := filepath.Base(filename)
	if strings.HasSuffix(base, ".tar.gz") || strings.HasSuffix(base, ".tgz") {
		return "tar.gz"
	}
	if strings.HasSuffix(base, ".tar.xz") || strings.HasSuffix(base, ".txz") {
		return "tar.xz"
	}
	if strings.HasSuffix(base, ".tar.bz2") || strings.HasSuffix(base, ".tbz2") {
		return "tar.bz2"
	}
	if strings.HasSuffix(base, ".zip") {
		return "zip"
	}
	return "tar.gz"
}
