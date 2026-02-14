package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Display the version, git commit, and build date of vmake.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("vmake %s\n", Version)
		fmt.Printf("  Git commit: %s\n", GitCommit)
		fmt.Printf("  Build date: %s\n", BuildDate)
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
	RootCmd.Version = Version
}
