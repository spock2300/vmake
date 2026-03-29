package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	iexec "gitee.com/spock2300/vmake/internal/exec"

	"github.com/spf13/cobra"
)

var (
	gitTagMinor  bool
	gitTagMajor  bool
	gitTagNoPush bool
	gitTagMsg    string
	gitTagYes    bool
)

var gitCmd = &cobra.Command{
	Use:   "git",
	Short: "Git version management commands",
}

var gitTagCmd = &cobra.Command{
	Use:   "tag [version]",
	Short: "Create version tag, update latest, and push",
	Long: `Create a new version tag, move the 'latest' tag to point to it, and push to remote.

Examples:
  vmake git tag              Bump patch version (v1.0.0 -> v1.0.1)
  vmake git tag --minor      Bump minor version (v1.0.0 -> v1.1.0)
  vmake git tag --major      Bump major version (v1.0.0 -> v2.0.0)
  vmake git tag v1.2.3       Create specific version
  vmake git tag --no-push    Create tags without pushing`,
	Args: cobra.MaximumNArgs(1),
	Run:  runGitTag,
}

func init() {
	gitTagCmd.Flags().BoolVar(&gitTagMinor, "minor", false, "bump minor version")
	gitTagCmd.Flags().BoolVar(&gitTagMajor, "major", false, "bump major version")
	gitTagCmd.Flags().BoolVar(&gitTagNoPush, "no-push", false, "create tags without pushing")
	gitTagCmd.Flags().StringVarP(&gitTagMsg, "message", "m", "", "custom tag message")
	gitTagCmd.Flags().BoolVarP(&gitTagYes, "yes", "y", false, "skip confirmation")

	gitCmd.AddCommand(gitTagCmd)
	RootCmd.AddCommand(gitCmd)
}

func runGitTag(cmd *cobra.Command, args []string) {
	fatalErr(checkStagedChanges())

	var newVersion string
	if len(args) > 0 {
		var err error
		newVersion, err = normalizeVersion(args[0])
		fatalErr(err)
	} else {
		latestTag, err := getLatestTag()
		fatalErr(err)
		newVersion, err = bumpVersion(latestTag, gitTagMajor, gitTagMinor)
		fatalErr(err)
	}

	msg := gitTagMsg
	if msg == "" {
		msg = fmt.Sprintf("Release %s", newVersion)
	}

	commit, err := iexec.Run("git", "rev-parse", "--short", "HEAD")
	fatalErr(err)

	pushInfo := "no (local only)"
	if !gitTagNoPush {
		remote, err := getRemoteName()
		if err == nil {
			pushInfo = fmt.Sprintf("yes (%s)", remote)
		} else {
			pushInfo = "yes (no remote found)"
		}
	}

	fmt.Printf("  Version:   %s\n", newVersion)
	fmt.Printf("  Message:   %s\n", msg)
	fmt.Printf("  Commit:    %s\n", strings.TrimSpace(string(commit)))
	fmt.Printf("  Push:      %s\n", pushInfo)

	if !gitTagYes {
		fmt.Printf("Confirm? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		answer, err := reader.ReadString('\n')
		if err != nil {
			fatalErr(fmt.Errorf("failed to read input: %w", err))
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Printf("Aborted.\n")
			return
		}
	}

	fmt.Printf("Creating tag %s...\n", newVersion)

	fatalErr(createAnnotatedTag(newVersion, msg))
	fmt.Printf("  Created tag: %s\n", newVersion)

	fatalErr(updateLatestTag())
	fmt.Printf("  Updated tag: latest -> %s\n", newVersion)

	if !gitTagNoPush {
		fatalErr(pushTags(newVersion))
		fmt.Printf("  Pushed to remote\n")
	}

	fmt.Printf("Done!\n")
}

func checkStagedChanges() error {
	_, err := iexec.Run("git", "diff", "--cached", "--quiet")
	if err != nil {
		return fmt.Errorf("staging area has uncommitted changes, please commit first")
	}
	return nil
}

func getLatestTag() (string, error) {
	output, err := iexec.Run("git", "tag", "--sort=-v:refname")
	if err != nil {
		return "", fmt.Errorf("failed to list tags: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "latest" {
			continue
		}
		if isValidVersion(line) {
			return line, nil
		}
	}

	return "v0.0.0", nil
}

func isValidVersion(s string) bool {
	matched, _ := regexp.MatchString(`^v\d+\.\d+\.\d+$`, s)
	return matched
}

func normalizeVersion(s string) (string, error) {
	matched, _ := regexp.MatchString(`^v?\d+\.\d+\.\d+$`, s)
	if !matched {
		return "", fmt.Errorf("invalid version format '%s', expected vX.Y.Z", s)
	}
	if !strings.HasPrefix(s, "v") {
		s = "v" + s
	}
	return s, nil
}

func bumpVersion(tag string, major, minor bool) (string, error) {
	tag = strings.TrimPrefix(tag, "v")
	if tag == "0.0.0" {
		return "v0.0.1", nil
	}

	parts := strings.Split(tag, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid version format: %s", tag)
	}

	majorNum, _ := strconv.Atoi(parts[0])
	minorNum, _ := strconv.Atoi(parts[1])
	patchNum, _ := strconv.Atoi(parts[2])

	if major {
		majorNum++
		minorNum = 0
		patchNum = 0
	} else if minor {
		minorNum++
		patchNum = 0
	} else {
		patchNum++
	}

	return fmt.Sprintf("v%d.%d.%d", majorNum, minorNum, patchNum), nil
}

func createAnnotatedTag(version, msg string) error {
	return iexec.RunToStdout("", "git", "tag", "-a", version, "-m", msg)
}

func updateLatestTag() error {
	return iexec.RunToStdout("", "git", "tag", "-f", "latest")
}

func pushTags(version string) error {
	remote, err := getRemoteName()
	if err != nil {
		return err
	}

	if err := iexec.RunToStdout("", "git", "push", "--atomic", remote, "HEAD", version); err != nil {
		return err
	}

	return iexec.RunToStdout("", "git", "push", "--force", remote, "latest")
}

func getRemoteName() (string, error) {
	output, err := iexec.Run("git", "remote")
	if err != nil {
		return "", fmt.Errorf("failed to get remote: %w", err)
	}

	remotes := strings.Fields(string(output))
	for _, r := range remotes {
		if r == "origin" {
			return "origin", nil
		}
	}

	if len(remotes) > 0 {
		return remotes[0], nil
	}

	return "", fmt.Errorf("no git remote found")
}
