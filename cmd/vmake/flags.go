package main

import (
	"github.com/spf13/cobra"
)

var (
	checkFlag bool
	planFlag  bool
)

func addInstallFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&installFlag, "install", "i", false, "install after build")
	cmd.Flags().StringVarP(&prefixFlag, "prefix", "p", "", "installation prefix (default: <project>/install)")
	cmd.Flags().StringVar(&installTypeFlag, "install-type", "runtime", "install type: runtime or sdk")
	cmd.RegisterFlagCompletionFunc("install-type", completeInstallType)
}

func addBuildFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&toolchainFlag, "toolchain", "", "override toolchain")
	cmd.Flags().StringVar(&modeFlag, "mode", "", "override build mode")
	cmd.Flags().StringVar(&manifestFlag, "manifest", "", "pin versions from manifest file")
	cmd.Flags().BoolVar(&testsFlag, "tests", false, "build test targets")
	cmd.Flags().IntVarP(&jobsFlag, "jobs", "j", 0, "compile jobs within the active target and Make/CMake ceiling; targets run serially (0 = number of CPUs, 1 = sequential)")
	cmd.Flags().BoolVarP(&keepGoingFlag, "keep-going", "k", false, "keep building independent targets after a failure")
	cmd.Flags().BoolVar(&explainFlag, "explain", false, "print why each compile/link action was rebuilt")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON result document on stdout (progress goes to stderr)")
	cmd.Flags().StringVar(&outputFlag, "output", "", "write one JSON result document to PATH instead of stdout (implies --json; - means stdout)")
	cmd.RegisterFlagCompletionFunc("toolchain", completeToolchain)
	cmd.RegisterFlagCompletionFunc("mode", completeMode)
}

// addCheckFlags registers plan mode on `vmake build`. Check resolves the
// project and reports pending compile/link/callback work without executing
// any action.
func addCheckFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&checkFlag, "check", false, "plan the next build and report pending compile/link/callback work without executing it")
	cmd.Flags().BoolVar(&planFlag, "plan", false, "alias for --check")
}

// addTestFlags registers the build flags for `vmake test`. `--tests` is kept
// for compatibility, but test always builds and runs test targets.
func addTestFlags(cmd *cobra.Command) {
	addBuildFlags(cmd)
	cmd.Flags().Lookup("tests").Usage = "implied and has no effect; vmake test always builds and runs test targets"
	cmd.Flags().Lookup("keep-going").Usage = "keep building independent targets after a failure; partial mode runs only the tests built by this invocation and records not-run/stale targets"
	cmd.Flags().BoolVar(&requireTestsFlag, "require-tests", false, "fail with exit code 1 when the project declares no runnable test targets (default: exit 0)")
}
