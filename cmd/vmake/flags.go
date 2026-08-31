package main

import (
	"github.com/spf13/cobra"
)

func addInstallFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&installFlag, "install", "i", false, "install after build")
	cmd.Flags().StringVarP(&prefixFlag, "prefix", "p", "", "installation prefix (default: ./install)")
	cmd.Flags().StringVar(&installTypeFlag, "install-type", "runtime", "install type: runtime or sdk")
	cmd.RegisterFlagCompletionFunc("install-type", completeInstallType)
}

func addBuildFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&toolchainFlag, "toolchain", "", "override toolchain")
	cmd.Flags().StringVar(&modeFlag, "mode", "", "override build mode")
	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "pin versions from manifest file")
	cmd.Flags().BoolVar(&testsFlag, "tests", false, "build test targets")
	cmd.Flags().IntVarP(&jobsFlag, "jobs", "j", 0, "parallel compile jobs per target (0 = number of CPUs)")
	cmd.Flags().BoolVarP(&keepGoingFlag, "keep-going", "k", false, "keep building independent targets after a failure")
	cmd.RegisterFlagCompletionFunc("toolchain", completeToolchain)
	cmd.RegisterFlagCompletionFunc("mode", completeMode)
}
