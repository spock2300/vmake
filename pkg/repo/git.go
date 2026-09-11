package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	exec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitcmd"
)

// runGit invokes git with the configuration vmake requires for reproducible
// checkouts; see internal/gitcmd.
func runGitCmd(args []string, opts exec.RunOptions) ([]byte, error) {
	return exec.RunWithOptions("git", gitcmd.Args(args...), opts)
}

func gitRun(dir string, args []string, timeout time.Duration) error {
	_, err := runGitCmd(args, exec.RunOptions{Dir: dir, Timeout: timeout, Quiet: true})
	if err != nil {
		return fmt.Errorf("git %s in %s: %w", args[0], dir, err)
	}
	return nil
}

func Clone(url, dir string) error {
	_, err := runGitCmd([]string{"clone", url, dir}, exec.RunOptions{
		Timeout: 5 * time.Minute, Quiet: true,
	})
	if err != nil {
		fs.RemoveIfExists(dir)
		return fmt.Errorf("git clone %s -> %s: %w", url, dir, err)
	}
	return nil
}

func InitSubmodules(dir string) error {
	_, err := runGitCmd([]string{"submodule", "update", "--init", "--recursive"}, exec.RunOptions{
		Dir: dir, Timeout: 10 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("git submodule update --init in %s: %w", dir, err)
	}
	return nil
}

func fetchTimeout() time.Duration {
	if s := os.Getenv("VMAKE_FETCH_TIMEOUT"); s != "" {
		if secs, err := strconv.Atoi(s); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 120 * time.Second
}

func FetchTags(dir string) error {
	return gitRun(dir, []string{"fetch", "--all", "--tags"}, fetchTimeout())
}

func Checkout(dir, ref string) error {
	return gitRun(dir, []string{"checkout", "--force", ref}, 0)
}

func FetchAndReset(dir string) error {
	if err := FetchTags(dir); err != nil {
		return err
	}
	return gitRun(dir, []string{"reset", "--hard", "origin/HEAD"}, 0)
}

func EnsureRepoAtRef(gitURL, repoDir, ref string) error {
	if ref != "" && IsAlreadyAtRef(repoDir, ref) {
		return nil
	}

	if !dirExists(repoDir) || !dirExists(filepath.Join(repoDir, ".git")) {
		if err := Clone(gitURL, repoDir); err != nil {
			return err
		}
	} else {
		if err := FetchTags(repoDir); err != nil {
			fs.RemoveIfExists(repoDir)
			if err := Clone(gitURL, repoDir); err != nil {
				return err
			}
		}
	}

	if ref == "" {
		return nil
	}

	return Checkout(repoDir, ref)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func Pull(dir string) error {
	return gitRun(dir, []string{"pull", "--ff-only"}, 2*time.Minute)
}

func ListTags(dir string) ([]string, error) {
	output, err := runGitCmd([]string{"tag", "--list"}, exec.RunOptions{Dir: dir, Quiet: true})
	if err != nil {
		return nil, fmt.Errorf("git tag --list in %s: %w", dir, err)
	}
	lines := strings.Split(exec.TrimOutput(output), "\n")
	var tags []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			tags = append(tags, line)
		}
	}
	return tags, nil
}

func GetCurrentCommit(dir string) (string, error) {
	output, err := runGitCmd([]string{"rev-parse", "HEAD"}, exec.RunOptions{Dir: dir, Quiet: true})
	if err != nil {
		return "", err
	}
	return exec.TrimOutput(output), nil
}

// ResolveCommit resolves a tag or ref to its commit SHA inside an existing
// clone. Used by manifest import to pin real commits without materializing.
func ResolveCommit(dir, ref string) (string, error) {
	output, err := runGitCmd([]string{"rev-parse", ref + "^{commit}"}, exec.RunOptions{Dir: dir, Quiet: true})
	if err != nil {
		return "", fmt.Errorf("resolve %s in %s: %w", ref, dir, err)
	}
	return exec.TrimOutput(output), nil
}

// ResolveRemoteCommit resolves a tag (or commit SHA) on a remote URL to its
// commit SHA via `git ls-remote`, without any local clone. Annotated tags are
// dereferenced (the ^{} pattern wins); a 40-hex ref that the remote does not
// know is taken as the commit itself.
func ResolveRemoteCommit(url, ref string) (string, error) {
	args := []string{"ls-remote", url, "refs/tags/" + ref + "^{}", "refs/tags/" + ref, ref}
	output, err := runGitCmd(args, exec.RunOptions{Timeout: fetchTimeout(), Quiet: true})
	if err != nil {
		return "", fmt.Errorf("ls-remote %s (%s): %w", url, ref, err)
	}
	commit := ""
	for _, line := range strings.Split(exec.TrimOutput(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if strings.HasSuffix(fields[1], "^{}") {
			return fields[0], nil
		}
		if commit == "" {
			commit = fields[0]
		}
	}
	if commit == "" && isCommitSHA(ref) {
		return ref, nil
	}
	if commit == "" {
		return "", fmt.Errorf("ref %s not found on %s", ref, url)
	}
	return commit, nil
}

func isCommitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// DescribeTag returns the tag exactly pointing at HEAD, or "" when HEAD is
// not tagged. Local-only; used to rebuild version->tag maps from cached
// checkouts without touching the network.
func DescribeTag(dir string) string {
	output, err := runGitCmd([]string{"describe", "--tags", "--exact-match", "HEAD"}, exec.RunOptions{Dir: dir, Quiet: true})
	if err != nil {
		return ""
	}
	return exec.TrimOutput(output)
}

func GitRevParse(dir string) string {
	commit, _ := GetCurrentCommit(dir)
	if commit == "" {
		return "unknown"
	}
	return commit
}

func IsAlreadyAtRef(dir, ref string) bool {
	head, err := GetCurrentCommit(dir)
	if err != nil {
		return false
	}
	output, err := runGitCmd([]string{"rev-parse", ref + "^{}"}, exec.RunOptions{Dir: dir, Quiet: true})
	if err != nil {
		return false
	}
	return head == exec.TrimOutput(output)
}

func IsPatchApplied(dir, patchFile string) bool {
	_, err := runGitCmd([]string{"apply", "--reverse", "--check", patchFile}, exec.RunOptions{Dir: dir})
	return err == nil
}

func ApplyPatch(dir, patchFile string) error {
	return gitRun(dir, []string{"apply", "--3way", patchFile}, 0)
}
