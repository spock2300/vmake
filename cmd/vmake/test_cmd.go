package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	addTestFlags(testCmd)
}

func runTest(cmd *cobra.Command, args []string) error {
	commandStorageLocks()
	return withBuildReport(func(report *buildReport) error {
		report.Command = reportCommandTest
		execution := &RuntimeContext{}
		return withResolvedProject(execution, func(ctx *RuntimeContext) error {
			if err := report.bind(ctx); err != nil {
				return err
			}
			result, err := runBuildPhase(ctx, BuildOptions{IncludeTests: true, Jobs: jobsFlag, KeepGoing: keepGoingFlag, Explain: explainFlag})
			if err != nil {
				if result != nil {
					report.partial(ctx, result, err)
				}
				return err
			}
			if err := report.complete(ctx, result); err != nil {
				return err
			}
			tests, err := runAllTests(ctx.Context, result)
			report.setTestResults(tests)
			return err
		})
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
	fullName    string
	packageName string
	targetName  string
	outputPath  string
	passed      bool
	elapsed     time.Duration
	exitCode    int
	output      string
}

const (
	buildTestOutputLimit  = 4096
	buildTestOutputMarker = "...[truncated]\n"
)

func boundedTestOutput(output string) string {
	output = strings.TrimSpace(output)
	if len(output) <= buildTestOutputLimit {
		return output
	}
	keep := buildTestOutputLimit - len(buildTestOutputMarker)
	return buildTestOutputMarker + output[len(output)-keep:]
}

func testExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *osexec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
	}
	return 1
}

func runAllTests(ctx context.Context, result *BuildResult) ([]testResult, error) {
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
			return nil, fmt.Errorf("test target %s has no resolved package platform", fullName)
		}
		if err := validateTestExecution(platform, runtime.GOOS); err != nil {
			return nil, err
		}

		outputPath := filepath.Join(pkgDirs.BuildDir, api.TargetFilename(node.Target.Kind(), node.Target.Name(), platform.OSOrHost()))
		if _, err := os.Stat(outputPath); err != nil {
			vlog.Error("FAIL %s (binary not found: %s)", fullName, outputPath)
			return nil, fmt.Errorf("test binary missing: %w", err)
		}

		tests = append(tests, testResult{
			fullName:    fullName,
			packageName: node.PkgName,
			targetName:  node.Target.Name(),
			outputPath:  outputPath,
		})
	}

	if len(tests) == 0 {
		return nil, fmt.Errorf("no test targets found")
	}

	vlog.Info("")
	vlog.Info("Running %d test(s)...", len(tests))

	for i := range tests {
		if err := ctx.Err(); err != nil {
			return tests[:i], err
		}
		t := &tests[i]
		vlog.Info("")
		vlog.Info("[%s]", t.fullName)

		start := time.Now()
		output, runErr := exec.RunWithOptions(t.outputPath, nil, exec.RunOptions{
			Dir:           filepath.Dir(t.outputPath),
			Context:       ctx,
			CaptureStderr: true,
		})
		if ctx.Err() != nil {
			return tests[:i], ctx.Err()
		}
		t.elapsed = time.Since(start)
		t.passed = runErr == nil
		t.exitCode = testExitCode(runErr)
		if commandErr, ok := runErr.(*exec.CommandError); ok {
			t.output = boundedTestOutput(commandErr.Output)
		} else {
			t.output = boundedTestOutput(string(output))
		}

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
		return tests, nil
	}
	fmt.Printf("%d/%d test(s) passed, %d failed.\n", passed, len(tests), len(tests)-passed)
	return tests, fmt.Errorf("%d test(s) failed", len(tests)-passed)
}
