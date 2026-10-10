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
	vlog "github.com/spock2300/vmake/pkg/log"
)

// runGit invokes git with the configuration vmake requires for reproducible
// checkouts; see internal/gitcmd.
func runGitCmd(args []string, opts exec.RunOptions) ([]byte, error) {
	return exec.RunWithOptions("git", gitcmd.Args(args...), opts)
}

// gitNetworkTimeout is the timeout for network git operations (clone, fetch,
// submodule init, ls-remote). VMAKE_GIT_TIMEOUT (seconds) overrides it;
// VMAKE_FETCH_TIMEOUT is accepted as a legacy alias. The default is generous
// because slow upstreams (e.g. GitHub from mainland China) can take a long
// time; successful commands finish early, and progress is streamed meanwhile.
func gitNetworkTimeout() time.Duration {
	for _, key := range []string{"VMAKE_GIT_TIMEOUT", "VMAKE_FETCH_TIMEOUT"} {
		if s := os.Getenv(key); s != "" {
			if secs, err := strconv.Atoi(s); err == nil && secs > 0 {
				return time.Duration(secs) * time.Second
			}
		}
	}
	return 30 * time.Minute
}

func networkGitOptions(ctx context.Context, dir string) exec.RunOptions {
	opts := exec.RunOptions{
		Context: ctx,
		Dir:     dir,
		Timeout: gitNetworkTimeout(),
		Quiet:   vlog.IsQuiet(),
	}
	if !opts.Quiet {
		// Show git's own progress lines without the built-in delay.
		opts.Env = map[string]string{"GIT_PROGRESS_DELAY": "0"}
	}
	return opts
}

// progressArgs appends git's forced progress flag unless output is quiet.
func progressArgs(args []string) []string {
	if vlog.IsQuiet() {
		return args
	}
	return append(args, "--progress")
}

// networkGitError adds a timeout hint for slow upstreams.
func networkGitError(action string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: timed out after %s (set VMAKE_GIT_TIMEOUT seconds to raise the limit): %w", action, gitNetworkTimeout(), err)
	}
	return fmt.Errorf("%s: %w", action, err)
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
	args := progressArgs(append([]string{"clone"}, flags...))
	args = append(args, url, dir)
	_, err := runGitCmd(args, networkGitOptions(ctx, ""))
	if err != nil {
		fs.RemoveIfExists(dir)
		return networkGitError(fmt.Sprintf("git clone %s -> %s", url, dir), err)
	}
	return nil
}

// CloneShallowContext clones the default branch of url with a one-commit
// history, leaving the working tree unchecked-out so the caller can detach at
// the exact commit it needs.
func CloneShallowContext(ctx context.Context, url, dir string) error {
	return cloneRepo(ctx, url, dir, []string{"--no-checkout", "--depth", "1"})
}

// FetchShallowCommitContext initializes dir as a repository tracking url and
// fetches exactly commit at depth 1. Used when only a commit id is known, for
// example manifest checkouts. Servers without allowReachableSHA1InWant cannot
// serve arbitrary commits; callers fall back to a full clone.
func FetchShallowCommitContext(ctx context.Context, url, dir, commit string) error {
	if err := initRepoContext(ctx, url, dir); err != nil {
		return err
	}
	return fetchShallowOriginContext(ctx, dir, commit)
}

// FetchShallowRefContext initializes dir as a repository tracking url and
// fetches localRef (e.g. refs/tags/v1) at depth 1, or the commit id when no
// ref name is known. It returns the local revision to check out.
func FetchShallowRefContext(ctx context.Context, url, dir, localRef, commit string) (string, error) {
	if err := initRepoContext(ctx, url, dir); err != nil {
		return "", err
	}
	if localRef != "" {
		target := localRef
		if strings.HasPrefix(localRef, "refs/heads/") {
			// Fetch branches into remote-tracking refs: the fresh repository's
			// unborn HEAD points at a local branch and git refuses to fetch
			// into the checked-out branch.
			target = "refs/remotes/origin/" + strings.TrimPrefix(localRef, "refs/heads/")
		}
		if err := fetchShallowOriginContext(ctx, dir, "+"+localRef+":"+target); err != nil {
			return "", err
		}
		return target, nil
	}
	if commit == "" {
		return "", fmt.Errorf("cannot fetch %s from %s", localRef, url)
	}
	if err := fetchShallowOriginContext(ctx, dir, commit); err != nil {
		return "", err
	}
	return commit, nil
}

func initRepoContext(ctx context.Context, url, dir string) error {
	if err := fs.EnsureDir(dir); err != nil {
		return err
	}
	if err := gitRunContext(ctx, dir, []string{"init"}, 0); err != nil {
		return err
	}
	return gitRunContext(ctx, dir, []string{"remote", "add", "origin", url}, 0)
}

func fetchShallowOriginContext(ctx context.Context, dir, spec string) error {
	args := progressArgs([]string{"fetch", "--depth", "1", "origin", spec})
	_, err := runGitCmd(args, networkGitOptions(ctx, dir))
	if err != nil {
		return networkGitError(fmt.Sprintf("git fetch %s in %s", spec, dir), err)
	}
	return nil
}

func revParseCommitContext(ctx context.Context, dir, rev string) (string, error) {
	output, err := runGitCmd([]string{"rev-parse", "--verify", rev + "^{commit}"}, exec.RunOptions{
		Context: ctx, Dir: dir, Quiet: true,
	})
	if err != nil {
		return "", fmt.Errorf("resolve %s in %s: %w", rev, dir, err)
	}
	return exec.TrimOutput(output), nil
}

// ListRemoteTags lists upstream tag names; see ListRemoteTagsContext.
func ListRemoteTags(urls []string) ([]string, error) {
	return ListRemoteTagsContext(context.Background(), urls)
}

// ListRemoteTagsContext lists upstream tag names, trying each URL in order.
// This is how versions are discovered without downloading any objects.
func ListRemoteTagsContext(ctx context.Context, urls []string) ([]string, error) {
	var lastErr error
	for _, url := range urls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		opts := networkGitOptions(ctx, "")
		opts.Quiet = true
		output, err := runGitCmd([]string{"ls-remote", "--tags", "--refs", url}, opts)
		if err != nil {
			lastErr = networkGitError(fmt.Sprintf("ls-remote --tags %s", url), err)
			continue
		}
		var tags []string
		for _, line := range strings.Split(exec.TrimOutput(output), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 || !strings.HasPrefix(fields[1], "refs/tags/") {
				continue
			}
			tags = append(tags, strings.TrimPrefix(fields[1], "refs/tags/"))
		}
		return tags, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no git URL configured")
	}
	return nil, lastErr
}

// ResolveRemoteHeadContext resolves the default-branch tip of the first
// reachable URL via `git ls-remote --symref`.
func ResolveRemoteHeadContext(ctx context.Context, urls []string) (string, error) {
	var lastErr error
	for _, url := range urls {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		opts := networkGitOptions(ctx, "")
		opts.Quiet = true
		output, err := runGitCmd([]string{"ls-remote", "--symref", url, "HEAD"}, opts)
		if err != nil {
			lastErr = networkGitError(fmt.Sprintf("ls-remote %s HEAD", url), err)
			continue
		}
		for _, line := range strings.Split(exec.TrimOutput(output), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[1] == "HEAD" {
				return fields[0], nil
			}
		}
		lastErr = fmt.Errorf("remote HEAD not found on %s", url)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no git URL configured")
	}
	return "", lastErr
}

// ResolveRemoteRefContext resolves a tag, branch or commit id on the first
// reachable URL to a commit id. It also returns the local ref name to fetch
// (empty for plain commit ids), so callers can materialize the exact
// namespace without probing. With an empty ref the remote HEAD is used.
func ResolveRemoteRefContext(ctx context.Context, urls []string, ref string) (string, string, error) {
	if ref == "" {
		commit, err := ResolveRemoteHeadContext(ctx, urls)
		return commit, "", err
	}
	var lastErr error
	for _, url := range urls {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		commit, localRef, err := resolveRemoteRefOne(ctx, url, ref)
		if err != nil {
			lastErr = err
			continue
		}
		return commit, localRef, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no git URL configured")
	}
	return "", "", lastErr
}

// resolveRemoteRefOne resolves ref on a single URL. Annotated tags prefer the
// peeled commit; tags win over branches of the same name. HEAD and fully
// qualified refs (refs/heads/x, refs/tags/x) resolve in their own namespace.
func resolveRemoteRefOne(ctx context.Context, url, ref string) (string, string, error) {
	opts := networkGitOptions(ctx, "")
	opts.Quiet = true
	var args []string
	switch {
	case ref == "HEAD":
		args = []string{"ls-remote", "--symref", url, "HEAD"}
	case strings.HasPrefix(ref, "refs/"):
		args = []string{"ls-remote", "--symref", url, ref + "^{}", ref}
	default:
		args = []string{"ls-remote", "--symref", url, "refs/tags/" + ref + "^{}", "refs/tags/" + ref, "refs/heads/" + ref}
	}
	output, err := runGitCmd(args, opts)
	if err != nil {
		return "", "", networkGitError(fmt.Sprintf("ls-remote %s (%s)", url, ref), err)
	}
	remote := make(map[string]string)
	for _, line := range strings.Split(exec.TrimOutput(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			remote[fields[1]] = fields[0]
		}
	}
	switch {
	case ref == "HEAD":
		if commit := remote["HEAD"]; commit != "" {
			return commit, "", nil
		}
	case strings.HasPrefix(ref, "refs/"):
		if commit := remote[ref+"^{}"]; commit != "" {
			return commit, ref, nil
		}
		if commit := remote[ref]; commit != "" {
			return commit, ref, nil
		}
	default:
		if commit := remote["refs/tags/"+ref+"^{}"]; commit != "" {
			return commit, "refs/tags/" + ref, nil
		}
		if commit := remote["refs/tags/"+ref]; commit != "" {
			return commit, "refs/tags/" + ref, nil
		}
		if commit := remote["refs/heads/"+ref]; commit != "" {
			return commit, "refs/heads/" + ref, nil
		}
	}
	if isCommitSHA(ref) {
		return ref, "", nil
	}
	return "", "", fmt.Errorf("ref %s not found on %s", ref, url)
}

// CheckoutDetachContext checks out an arbitrary commit in a detached HEAD.
func CheckoutDetachContext(ctx context.Context, dir, commit string) error {
	return gitRunContext(ctx, dir, []string{"checkout", "--force", "--detach", commit}, 0)
}

func InitSubmodules(dir string) error {
	return InitSubmodulesContext(context.Background(), dir)
}

func InitSubmodulesContext(ctx context.Context, dir string) error {
	args := progressArgs([]string{"submodule", "update", "--init", "--depth", "1", "--recursive"})
	_, err := runGitCmd(args, networkGitOptions(ctx, dir))
	if err != nil {
		return networkGitError(fmt.Sprintf("git submodule update --init in %s", dir), err)
	}
	return nil
}

func FetchTags(dir string) error {
	return FetchTagsContext(context.Background(), dir)
}

func FetchTagsContext(ctx context.Context, dir string) error {
	args := progressArgs([]string{"fetch", "--all", "--tags"})
	_, err := runGitCmd(args, networkGitOptions(ctx, dir))
	if err != nil {
		return networkGitError(fmt.Sprintf("git fetch --all --tags in %s", dir), err)
	}
	return nil
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
	opts := networkGitOptions(ctx, "")
	opts.Quiet = true
	output, err := runGitCmd(args, opts)
	if err != nil {
		return "", networkGitError(fmt.Sprintf("ls-remote %s (%s)", url, ref), err)
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

func validCommitID(commit string) bool {
	return (len(commit) == 40 || len(commit) == 64) && strings.Trim(commit, "0123456789abcdef") == ""
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
