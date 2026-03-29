package repo

import (
	"fmt"
	"os"
	"time"

	exec "gitee.com/spock2300/vmake/internal/exec"
)

func gitRun(dir string, args []string, timeout time.Duration) error {
	_, err := exec.RunWithOptions("git", args, exec.RunOptions{Dir: dir, Timeout: timeout})
	if err != nil {
		return fmt.Errorf("git %s in %s: %w", args[0], dir, err)
	}
	return nil
}

func Clone(url, dir string) error {
	_, err := exec.RunWithOptions("git", []string{"clone", url, dir}, exec.RunOptions{
		Timeout: 5 * time.Minute,
	})
	if err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("git clone %s -> %s: %w", url, dir, err)
	}
	return nil
}

func InitSubmodules(dir string) error {
	return gitRun(dir, []string{"submodule", "update", "--init", "--recursive"}, 2*time.Minute)
}

func FetchTags(dir string) error {
	return gitRun(dir, []string{"fetch", "--all", "--tags"}, 30*time.Second)
}

func Checkout(dir, ref string) error {
	return gitRun(dir, []string{"checkout", "--force", ref}, 0)
}

func FetchAndReset(dir string) error {
	if err := gitRun(dir, []string{"fetch", "--all", "--tags"}, 0); err != nil {
		return err
	}
	return gitRun(dir, []string{"reset", "--hard", "origin/HEAD"}, 0)
}

func Pull(dir string) error {
	return gitRun(dir, []string{"pull", "--ff-only"}, 2*time.Minute)
}

func GetCurrentCommit(dir string) (string, error) {
	output, err := exec.RunWithOptions("git", []string{"rev-parse", "HEAD"}, exec.RunOptions{Dir: dir})
	if err != nil {
		return "", err
	}
	return exec.TrimOutput(output), nil
}

func IsAlreadyAtRef(dir, ref string) bool {
	head, err := GetCurrentCommit(dir)
	if err != nil {
		return false
	}
	output, err := exec.RunWithOptions("git", []string{"rev-parse", ref + "^{}"}, exec.RunOptions{Dir: dir})
	if err != nil {
		return false
	}
	return head == exec.TrimOutput(output)
}

func IsPatchApplied(dir, patchFile string) bool {
	_, err := exec.RunWithOptions("git", []string{"apply", "--reverse", "--check", patchFile}, exec.RunOptions{Dir: dir})
	return err == nil
}

func ApplyPatch(dir, patchFile string) error {
	return gitRun(dir, []string{"apply", "--3way", patchFile}, 0)
}
