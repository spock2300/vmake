package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	exec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Build and run test targets",
	RunE:  runTest,
}

func init() {
	RootCmd.AddCommand(testCmd)
	addBuildFlags(testCmd)
}

func runTest(cmd *cobra.Command, args []string) error {
	commandStorageLocks()
	execution := &RuntimeContext{}
	return withBuildContext(execution, func() error {
		ctx, err := resolveToConfigContext(execution.Context, false)
		if err != nil {
			return err
		}
		result, err := runBuildPhase(ctx, BuildOptions{IncludeTests: true, Jobs: jobsFlag, KeepGoing: keepGoingFlag})
		if err != nil {
			return err
		}
		return runAllTests(ctx.Context, result)
	})
}

func validateTestExecution(platform api.Platform, hostOS string) error {
	targetOS := platform.OSOrHost()
	if targetOS == "none" || targetOS != hostOS || platform.Triple != "" {
		return fmt.Errorf("cannot run tests for project target (target_os=%s, target_triple=%q) on %s; use 'vmake build --tests' to build them without execution", targetOS, platform.Triple, hostOS)
	}
	return nil
}

type testResult struct {
	fullName   string
	outputPath string
	passed     bool
	elapsed    time.Duration
}

func runAllTests(ctx context.Context, result *BuildResult) error {
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
		platform, ok := result.PkgPlatforms[node.PkgName]
		if !ok {
			return fmt.Errorf("test target %s has no resolved package platform", fullName)
		}
		if err := validateTestExecution(platform, runtime.GOOS); err != nil {
			return err
		}

		outputPath := filepath.Join(pkgDirs.BuildDir, api.TargetFilename(node.Target.Kind(), node.Target.Name(), platform.OSOrHost()))
		if _, err := os.Stat(outputPath); err != nil {
			vlog.Error("FAIL %s (binary not found: %s)", fullName, outputPath)
			return fmt.Errorf("test binary missing: %w", err)
		}

		tests = append(tests, testResult{
			fullName:   fullName,
			outputPath: outputPath,
		})
	}

	if len(tests) == 0 {
		vlog.Info("No test targets found.")
		return nil
	}

	vlog.Info("")
	vlog.Info("Running %d test(s)...", len(tests))

	for i := range tests {
		if err := ctx.Err(); err != nil {
			return err
		}
		t := &tests[i]
		vlog.Info("")
		vlog.Info("[%s]", t.fullName)

		start := time.Now()
		err := exec.RunWithEnvContext(ctx, filepath.Dir(t.outputPath), nil, t.outputPath)
		if ctx.Err() != nil {
			return ctx.Err()
		}
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
		return fmt.Errorf("%d test(s) failed", len(tests)-passed)
	}
	return nil
}
