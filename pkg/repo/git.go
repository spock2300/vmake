package repo

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func Clone(url, dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "clone", url, dir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone %s -> %s: %w", url, dir, err)
	}
	return nil
}

func FetchTags(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "fetch", "--all", "--tags")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git fetch in %s: %w", dir, err)
	}
	return nil
}

func Checkout(dir, ref string) error {
	cmd := exec.Command("git", "checkout", "--force", ref)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git checkout %s in %s: %w", ref, dir, err)
	}
	return nil
}

func FetchAndReset(dir string) error {
	cmds := [][]string{
		{"git", "fetch", "--all", "--tags"},
		{"git", "reset", "--hard", "origin/HEAD"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%v in %s: %w", args, dir, err)
		}
	}
	return nil
}

func GetCurrentCommit(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func GetCurrentTag(dir string) (string, error) {
	cmd := exec.Command("git", "describe", "--tags", "--exact-match")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func ListTags(dir string) ([]string, error) {
	cmd := exec.Command("git", "tag", "-l")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	tags := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(tags) == 1 && tags[0] == "" {
		return nil, nil
	}
	return tags, nil
}
