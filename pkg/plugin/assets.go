package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"gitee.com/spock2300/vmake/internal/fs"
)

func RunGitLFS(repoDir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"lfs"}, args...)...)
	cmd.Dir = repoDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git lfs failed: %w", err)
	}

	return nil
}

func PullLFSFiles(repoDir string, files ...string) error {
	if len(files) == 0 {
		return nil
	}

	args := []string{"pull", "--include"}
	args = append(args, strings.Join(files, ","))
	return RunGitLFS(repoDir, args...)
}

func DownloadFile(url, dest string) error {
	if err := fs.EnsureParentDir(dest); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	cmd := exec.Command("curl", "-L", "-o", dest, url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}

func ExtractArchive(archive, dest string) error {
	if err := fs.EnsureDir(dest); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	cmd := exec.Command("tar", "-xzf", archive, "-C", dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	return nil
}
