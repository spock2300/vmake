package main

import (
	"fmt"

	iexec "gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/pkg/version"
	"github.com/spf13/cobra"
)

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

	goCmd, err := iexec.LookPath("go")
	fatalErr(err)

	fatalErr(iexec.RunToStdout("", goCmd, "install", pkg))

	fmt.Printf("Update to %s completed!\n", targetVer)
}
