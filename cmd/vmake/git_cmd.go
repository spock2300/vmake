package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	gitTagMinor  bool
	gitTagMajor  bool
	gitTagNoPush bool
	gitTagMsg    string
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

	gitCmd.AddCommand(gitTagCmd)
	RootCmd.AddCommand(gitCmd)
}

func runGitTag(cmd *cobra.Command, args []string) {
	if err := checkGitClean(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var newVersion string
	if len(args) > 0 {
		var err error
		newVersion, err = normalizeVersion(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		latestTag, err := getLatestTag()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		newVersion, err = bumpVersion(latestTag, gitTagMajor, gitTagMinor)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("Creating tag %s...\n", newVersion)

	msg := gitTagMsg
	if msg == "" {
		msg = fmt.Sprintf("Release %s", newVersion)
	}

	if err := createAnnotatedTag(newVersion, msg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Created tag: %s\n", newVersion)

	if err := updateLatestTag(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Updated tag: latest -> %s\n", newVersion)

	if !gitTagNoPush {
		if err := pushTags(newVersion); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  Pushed to remote\n")
	}

	fmt.Printf("Done!\n")
}

func checkGitClean() error {
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}
	if len(output) > 0 {
		return fmt.Errorf("working directory is not clean, please commit or stash changes first")
	}
	return nil
}

func getLatestTag() (string, error) {
	cmd := exec.Command("git", "tag", "--sort=-v:refname")
	output, err := cmd.Output()
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
	cmd := exec.Command("git", "tag", "-a", version, "-m", msg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func updateLatestTag() error {
	cmd := exec.Command("git", "tag", "-f", "latest")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func pushTags(version string) error {
	remote, err := getRemoteName()
	if err != nil {
		return err
	}

	cmd := exec.Command("git", "push", "--atomic", remote, "HEAD", version, "latest")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func getRemoteName() (string, error) {
	cmd := exec.Command("git", "remote")
	output, err := cmd.Output()
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
