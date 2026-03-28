package repo

import (
	"fmt"
	"os"
	"time"

	exec "gitee.com/spock2300/vmake/internal/exec"
)

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
	_, err := exec.RunWithOptions("git", []string{"submodule", "update", "--init", "--recursive"}, exec.RunOptions{
		Dir:     dir,
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("git submodule init in %s: %w", dir, err)
	}
	return nil
}

func FetchTags(dir string) error {
	_, err := exec.RunWithOptions("git", []string{"fetch", "--all", "--tags"}, exec.RunOptions{
		Dir:     dir,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("git fetch in %s: %w", dir, err)
	}
	return nil
}

func Checkout(dir, ref string) error {
	_, err := exec.RunWithOptions("git", []string{"checkout", "--force", ref}, exec.RunOptions{
		Dir: dir,
	})
	if err != nil {
		return fmt.Errorf("git checkout %s in %s: %w", ref, dir, err)
	}
	return nil
}

func FetchAndReset(dir string) error {
	cmds := [][]string{
		{"fetch", "--all", "--tags"},
		{"reset", "--hard", "origin/HEAD"},
	}
	for _, args := range cmds {
		_, err := exec.RunWithOptions("git", args, exec.RunOptions{Dir: dir})
		if err != nil {
			return fmt.Errorf("git %v in %s: %w", args[0], dir, err)
		}
	}
	return nil
}

func Pull(dir string) error {
	_, err := exec.RunWithOptions("git", []string{"pull", "--ff-only"}, exec.RunOptions{
		Dir:     dir,
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("git pull in %s: %w", dir, err)
	}
	return nil
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
	_, err := exec.RunWithOptions("git", []string{"apply", "--3way", patchFile}, exec.RunOptions{Dir: dir})
	if err != nil {
		return fmt.Errorf("git apply %s in %s: %w", patchFile, dir, err)
	}
	return nil
}
