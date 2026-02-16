package main

import (
	"fmt"
	"os"
	"os/exec"

	"gitee.com/spock2300/vmake/pkg/version"
	"github.com/spf13/cobra"
)

var updateVersion string

var updateCmd = &cobra.Command{
	Use:   "update [version]",
	Short: "Update vmake to latest or specified version",
	Long: `Update vmake using go install.

Examples:
  vmake update          Update to latest version
  vmake update v1.2.3   Update to specific version`,
	Run: runUpdate,
}

func init() {
	RootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) {
	targetVer := "latest"
	if len(args) > 0 {
		targetVer = args[0]
	}

	fmt.Printf("Current version: %s\n", version.Version)
	fmt.Printf("Installing vmake@%s...\n", targetVer)

	pkg := fmt.Sprintf("gitee.com/spock2300/vmake/cmd/vmake@%s", targetVer)

	goCmd, err := exec.LookPath("go")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: go command not found: %v\n", err)
		os.Exit(1)
	}

	installCmd := exec.Command(goCmd, "install", pkg)
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr

	if err := installCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: update failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Update to %s completed!\n", targetVer)
}
