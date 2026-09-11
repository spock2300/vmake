package plugin

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitcmd"
)

func RunGitLFS(repoDir string, args ...string) error {
	fullArgs := append([]string{"lfs"}, args...)
	if err := iexec.RunToStdout(repoDir, "git", gitcmd.Args(fullArgs...)...); err != nil {
		return fmt.Errorf("git lfs failed: %w", err)
	}
	return nil
}

func DownloadFile(url, dest string) error {
	if err := fs.EnsureParentDir(dest); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := iexec.RunToStdout("", "curl", "-L", "-o", msysPath(dest), url); err != nil {
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
		return runExtract("tar", "-xzf", msysPath(archive), "-C", msysPath(dest))
	case "tar.xz", "txz":
		return runExtract("tar", "-xJf", msysPath(archive), "-C", msysPath(dest))
	case "tar.bz2", "tbz2":
		return runExtract("tar", "-xjf", msysPath(archive), "-C", msysPath(dest))
	case "zip":
		return extractZip(archive, dest)
	default:
		return runExtract("tar", "-xzf", msysPath(archive), "-C", msysPath(dest))
	}
}

// msysPath converts a path handed to an MSYS binary (tar from Git for
// Windows). The MSYS runtime de-quotes backslashes in argv, so Windows paths
// must arrive slash-separated. No-op on other platforms.
func msysPath(p string) string {
	return filepath.ToSlash(p)
}

// extractZip unpacks a zip archive with the standard library. Git for Windows
// only bundles unzip in the full installer, and archive/zip removes the
// dependency entirely.
func extractZip(archive, dest string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", archive, err)
	}
	defer r.Close()

	for _, f := range r.File {
		if err := extractZipEntry(f, dest); err != nil {
			return err
		}
	}
	return nil
}

func extractZipEntry(f *zip.File, dest string) error {
	target := filepath.Join(dest, filepath.FromSlash(f.Name))

	rel, err := filepath.Rel(dest, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("zip entry %q escapes the destination directory", f.Name)
	}

	if f.FileInfo().IsDir() {
		return fs.EnsureDir(target)
	}
	if err := fs.EnsureParentDir(target); err != nil {
		return err
	}

	src, err := f.Open()
	if err != nil {
		return fmt.Errorf("read zip entry %s: %w", f.Name, err)
	}
	defer src.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}

	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return fmt.Errorf("extract %s: %w", f.Name, err)
	}
	return out.Close()
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
