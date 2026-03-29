package main

import (
	"github.com/spf13/cobra"
)

func addInstallFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&installFlag, "install", "i", false, "install after build")
	cmd.Flags().StringVarP(&prefixFlag, "prefix", "p", "", "installation prefix (default: ./install)")
	cmd.Flags().StringVar(&installTypeFlag, "install-type", "runtime", "install type: runtime or sdk")
}
