package plugin

import (
	"fmt"

	iexec "gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/internal/fs"
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

func ExtractArchive(archive, dest string) error {
	if err := fs.EnsureDir(dest); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := iexec.RunToStdout("", "tar", "-xzf", archive, "-C", dest); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	return nil
}
