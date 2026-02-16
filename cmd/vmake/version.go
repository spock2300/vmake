package main

import (
	"fmt"

	"gitee.com/spock2300/vmake/pkg/version"
	"github.com/spf13/cobra"
)

var (
	GitCommit = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Display the version, git commit, and build date of vmake.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("vmake %s\n", version.Version)
		fmt.Printf("  Git commit: %s\n", GitCommit)
		fmt.Printf("  Build date: %s\n", BuildDate)
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
	RootCmd.Version = version.Version
}
