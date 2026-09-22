package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	exec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/toolchain"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Build and run test targets",
	Run:   runTest,
}

func init() {
	RootCmd.AddCommand(testCmd)
	addBuildFlags(testCmd)
}

func runTest(cmd *cobra.Command, args []string) {
	ctx := resolveToConfig(false)
	tc, _, err := pipeline.GetToolchain(ctx.Config, ctx.ToolchainOverride)
	fatalErr(err)
	fatalErr(validateTestExecution(tc, runtime.GOOS))
	result, err := runBuildPhase(ctx, BuildOptions{IncludeTests: true, Jobs: jobsFlag, KeepGoing: keepGoingFlag})
	fatalErr(err)
	runAllTests(result)
}

func validateTestExecution(tc *toolchain.Toolchain, hostOS string) error {
	targetOS := toolchain.TargetOSOf(tc)
	if targetOS == "none" || targetOS != hostOS || tc.TargetTriple != "" {
		return fmt.Errorf("cannot run tests for toolchain %q (target_os=%s, target_triple=%q) on %s; use 'vmake build --tests' to build them without execution", tc.Name, targetOS, tc.TargetTriple, hostOS)
	}
	return nil
}

type testResult struct {
	fullName   string
	outputPath string
	passed     bool
	elapsed    time.Duration
}

func runAllTests(result *BuildResult) {
	var tests []testResult

	for _, fullName := range result.Graph.Order {
		node, err := result.Graph.GetNode(fullName)
		if err != nil {
			continue
		}
		if !node.Target.IsTest() || !node.Target.IsDefault() {
			continue
		}
		if node.Target.Kind() != api.TargetBinary {
			continue
		}

		pkgDirs, ok := result.PkgDirs[node.PkgName]
		if !ok {
			continue
		}
		buildKey, ok := result.PkgBuildKeys[node.PkgName]
		if !ok {
			continue
		}

		outputPath := build.TargetOutputPath(pkgDirs.SourceDir, buildKey, node.Target.Kind(), node.Target.Name(), result.TargetOS)
		if _, err := os.Stat(outputPath); err != nil {
			vlog.Error("FAIL %s (binary not found: %s)", fullName, outputPath)
			os.Exit(1)
		}

		tests = append(tests, testResult{
			fullName:   fullName,
			outputPath: outputPath,
		})
	}

	if len(tests) == 0 {
		vlog.Info("No test targets found.")
		return
	}

	vlog.Info("")
	vlog.Info("Running %d test(s)...", len(tests))

	for i := range tests {
		t := &tests[i]
		vlog.Info("")
		vlog.Info("[%s]", t.fullName)

		start := time.Now()
		err := exec.RunToStdout(filepath.Dir(t.outputPath), t.outputPath)
		t.elapsed = time.Since(start)
		t.passed = err == nil

		if t.passed {
			vlog.Info("PASS %s (%s)", t.fullName, t.elapsed.Round(time.Millisecond))
		} else {
			vlog.Error("FAIL %s (%s)", t.fullName, t.elapsed.Round(time.Millisecond))
		}
	}

	vlog.Info("")
	passed := 0
	for _, t := range tests {
		if t.passed {
			passed++
		}
	}

	if passed == len(tests) {
		fmt.Printf("%d/%d test(s) passed.\n", passed, len(tests))
	} else {
		fmt.Printf("%d/%d test(s) passed, %d failed.\n", passed, len(tests), len(tests)-passed)
		os.Exit(1)
	}
}
