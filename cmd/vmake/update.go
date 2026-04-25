package main

import (
	"fmt"
	"strings"

	iexec "gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/version"
	"github.com/spf13/cobra"
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
		versions, err := listAvailableVersions(goCmd)
		fatalErr(err)
		targetVer = pickLatestStable(versions)
		fmt.Printf("Available versions: %s\n", strings.Join(versions, " "))
		fmt.Printf("Selected latest: %s\n", targetVer)
	}

	fmt.Printf("Installing vmake@%s...\n", targetVer)
	fatalErr(iexec.RunToStdout("", goCmd, "install", fmt.Sprintf("%s@%s", modulePath, targetVer)))
	fmt.Printf("Update to %s completed!\n", targetVer)
}

func listAvailableVersions(goCmd string) ([]string, error) {
	output, err := iexec.RunWithOptions(goCmd, []string{"list", "-m", "-versions", "gitee.com/spock2300/vmake"}, iexec.RunOptions{Quiet: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list versions: %w", err)
	}
	parts := strings.Fields(iexec.TrimOutput(output))
	if len(parts) <= 1 {
		return nil, fmt.Errorf("no published versions found")
	}
	return parts[1:], nil
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
