package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	iexec "gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/version"
)

const modulePath = "gitee.com/spock2300/vmake/cmd/vmake"

var updateCmd = &cobra.Command{
	Use:   "update [version]",
	Short: "Update vmake to latest or specified version",
	Long: `Update vmake using go install.

Examples:
  vmake update          Update to latest stable version
  vmake update v1.2.3   Update to specific version`,
	Run: runUpdate,
}

func init() {
	RootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) {
	fmt.Printf("Current version: %s\n", version.Version)

	goCmd, err := iexec.LookPath("go")
	fatalErr(err)

	targetVer := ""
	if len(args) > 0 {
		targetVer = args[0]
	} else {
		versions, err := listAvailableVersions()
		fatalErr(err)
		targetVer = pickLatestStable(versions)
		fmt.Printf("Available versions: %s\n", strings.Join(versions, " "))
		fmt.Printf("Selected latest: %s\n", targetVer)
	}

	fmt.Printf("Installing vmake@%s...\n", targetVer)
	fatalErr(iexec.RunToStdout("", goCmd, "install", fmt.Sprintf("%s@%s", modulePath, targetVer)))
	fmt.Printf("Update to %s completed!\n", targetVer)
}

func listAvailableVersions() ([]string, error) {
	gitCmd, err := iexec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git not found: %w", err)
	}
	output, err := iexec.RunWithOptions(gitCmd, []string{"ls-remote", "--tags", "https://gitee.com/spock2300/vmake.git"}, iexec.RunOptions{Quiet: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list remote tags: %w", err)
	}
	var tags []string
	for _, line := range strings.Split(iexec.TrimOutput(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, "^{}") {
			continue
		}
		idx := strings.Index(line, "refs/tags/")
		if idx < 0 {
			continue
		}
		tag := line[idx+len("refs/tags/"):]
		tags = append(tags, tag)
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("no published versions found")
	}
	return tags, nil
}

func pickLatestStable(versions []string) string {
	var stable []string
	for _, v := range versions {
		pv, ok := api.ParseVersion(v)
		if ok && pv.Pre == "" {
			stable = append(stable, v)
		}
	}
	result, ok := api.MatchVersion(stable, ">=0.0.0")
	if !ok {
		fatalMsg("no stable version available")
	}
	return result
}
