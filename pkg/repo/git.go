package repo

import (
	"context"
	"errors"
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
	return gitRunContext(context.Background(), dir, args, timeout)
}

func gitRunContext(ctx context.Context, dir string, args []string, timeout time.Duration) error {
	_, err := runGitCmd(args, exec.RunOptions{Context: ctx, Dir: dir, Timeout: timeout, Quiet: true})
	if err != nil {
		return fmt.Errorf("git %s in %s: %w", args[0], dir, err)
	}
	return nil
}

func Clone(url, dir string) error {
	return CloneContext(context.Background(), url, dir)
}

func CloneContext(ctx context.Context, url, dir string) error {
	return cloneRepo(ctx, url, dir, nil)
}

func CloneHead(url, dir string) error {
	return CloneHeadContext(context.Background(), url, dir)
}

func CloneHeadContext(ctx context.Context, url, dir string) error {
	return cloneRepo(ctx, url, dir, []string{"--depth", "1", "--single-branch", "--no-tags"})
}

func cloneRepo(ctx context.Context, url, dir string, flags []string) error {
	args := append([]string{"clone"}, flags...)
	args = append(args, url, dir)
	_, err := runGitCmd(args, exec.RunOptions{
		Context: ctx, Timeout: 5 * time.Minute, Quiet: true,
	})
	if err != nil {
		fs.RemoveIfExists(dir)
		return fmt.Errorf("git clone %s -> %s: %w", url, dir, err)
	}
	return nil
}

func InitSubmodules(dir string) error {
	return InitSubmodulesContext(context.Background(), dir)
}

func InitSubmodulesContext(ctx context.Context, dir string) error {
	_, err := runGitCmd([]string{"submodule", "update", "--init", "--recursive"}, exec.RunOptions{
		Context: ctx, Dir: dir, Timeout: 10 * time.Minute,
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
	return FetchTagsContext(context.Background(), dir)
}

func FetchTagsContext(ctx context.Context, dir string) error {
	return gitRunContext(ctx, dir, []string{"fetch", "--all", "--tags"}, fetchTimeout())
}

func Checkout(dir, ref string) error {
	return CheckoutContext(context.Background(), dir, ref)
}

func CheckoutContext(ctx context.Context, dir, ref string) error {
	return gitRunContext(ctx, dir, []string{"checkout", "--force", ref}, 0)
}

func FetchAndReset(dir string) error {
	return FetchAndResetContext(context.Background(), dir)
}

func FetchAndResetContext(ctx context.Context, dir string) error {
	if err := FetchTagsContext(ctx, dir); err != nil {
		return err
	}
	return gitRunContext(ctx, dir, []string{"reset", "--hard", "origin/HEAD"}, 0)
}

func EnsureRepoAtRef(gitURL, repoDir, ref string) error {
	return EnsureRepoAtRefContext(context.Background(), gitURL, repoDir, ref)
}

func EnsureRepoAtRefContext(ctx context.Context, gitURL, repoDir, ref string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if ref != "" {
		already, err := IsAlreadyAtRefContext(ctx, repoDir, ref)
		if err != nil {
			return err
		}
		if already {
			return nil
		}
	}

	if !dirExists(repoDir) || !dirExists(filepath.Join(repoDir, ".git")) {
		if err := CloneContext(ctx, gitURL, repoDir); err != nil {
			return err
		}
	} else {
		if err := FetchTagsContext(ctx, repoDir); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fs.RemoveIfExists(repoDir)
			if err := CloneContext(ctx, gitURL, repoDir); err != nil {
				return err
			}
		}
	}

	if ref == "" {
		return nil
	}

	return CheckoutContext(ctx, repoDir, ref)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func Pull(dir string) error {
	return gitRun(dir, []string{"pull", "--ff-only"}, 2*time.Minute)
}

func ListTags(dir string) ([]string, error) {
	return ListTagsContext(context.Background(), dir)
}

func ListTagsContext(ctx context.Context, dir string) ([]string, error) {
	output, err := runGitCmd([]string{"tag", "--list"}, exec.RunOptions{Context: ctx, Dir: dir, Quiet: true})
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
	return GetCurrentCommitContext(context.Background(), dir)
}

func GetCurrentCommitContext(ctx context.Context, dir string) (string, error) {
	output, err := runGitCmd([]string{"rev-parse", "HEAD"}, exec.RunOptions{Context: ctx, Dir: dir, Quiet: true})
	if err != nil {
		return "", err
	}
	return exec.TrimOutput(output), nil
}

// ResolveCommit resolves a tag or ref to its commit SHA inside an existing
// clone. Used by manifest import to pin real commits without materializing.
func ResolveCommit(dir, ref string) (string, error) {
	return ResolveCommitContext(context.Background(), dir, ref)
}

func ResolveCommitContext(ctx context.Context, dir, ref string) (string, error) {
	output, err := runGitCmd([]string{"rev-parse", ref + "^{commit}"}, exec.RunOptions{Context: ctx, Dir: dir, Quiet: true})
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
	return ResolveRemoteCommitContext(context.Background(), url, ref)
}

func ResolveRemoteCommitContext(ctx context.Context, url, ref string) (string, error) {
	args := []string{"ls-remote", url, "refs/tags/" + ref + "^{}", "refs/tags/" + ref, ref}
	output, err := runGitCmd(args, exec.RunOptions{Context: ctx, Timeout: fetchTimeout(), Quiet: true})
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
	return validCommitID(s)
}

// DescribeTag returns the tag exactly pointing at HEAD, or "" when HEAD is
// not tagged. Local-only; used to rebuild version->tag maps from cached
// checkouts without touching the network.
func DescribeTag(dir string) string {
	tag, _ := DescribeTagContext(context.Background(), dir)
	return tag
}

func DescribeTagContext(ctx context.Context, dir string) (string, error) {
	output, err := runGitCmd([]string{"describe", "--tags", "--exact-match", "HEAD"}, exec.RunOptions{Context: ctx, Dir: dir, Quiet: true})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", nil
	}
	return exec.TrimOutput(output), nil
}

func GitRevParse(dir string) string {
	commit, _ := GetCurrentCommit(dir)
	if commit == "" {
		return "unknown"
	}
	return commit
}

func IsAlreadyAtRef(dir, ref string) bool {
	already, _ := IsAlreadyAtRefContext(context.Background(), dir, ref)
	return already
}

func IsAlreadyAtRefContext(ctx context.Context, dir, ref string) (bool, error) {
	head, err := GetCurrentCommitContext(ctx, dir)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, err
		}
		return false, nil
	}
	output, err := runGitCmd([]string{"rev-parse", ref + "^{}"}, exec.RunOptions{Context: ctx, Dir: dir, Quiet: true})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, err
		}
		return false, nil
	}
	return head == exec.TrimOutput(output), nil
}

func IsPatchApplied(dir, patchFile string) bool {
	applied, _ := IsPatchAppliedContext(context.Background(), dir, patchFile)
	return applied
}

func IsPatchAppliedContext(ctx context.Context, dir, patchFile string) (bool, error) {
	_, err := runGitCmd([]string{"apply", "--reverse", "--check", patchFile}, exec.RunOptions{Context: ctx, Dir: dir, Quiet: true})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false, err
	}
	return err == nil, nil
}

func ApplyPatch(dir, patchFile string) error {
	return ApplyPatchContext(context.Background(), dir, patchFile)
}

func ApplyPatchContext(ctx context.Context, dir, patchFile string) error {
	if err := gitRunContext(ctx, dir, []string{"update-index", "-q", "--refresh"}, 0); err != nil {
		return err
	}
	return gitRunContext(ctx, dir, []string{"apply", "--3way", patchFile}, 0)
}
