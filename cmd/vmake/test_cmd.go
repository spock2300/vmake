package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	exec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
)

var requireTestsFlag bool

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
	return withReport(reportCommandTest, func(report *buildReport) error {
		execution := &RuntimeContext{}
		return withResolvedProject(execution, func(ctx *RuntimeContext) error {
			if err := report.bind(ctx); err != nil {
				return err
			}
			if err := validateTestProject(ctx); err != nil {
				return err
			}
			result, err := runBuildPhase(ctx, BuildOptions{IncludeTests: true, Jobs: jobsFlag, KeepGoing: keepGoingFlag, Explain: explainFlag})
			if err != nil {
				if result != nil {
					report.partial(ctx, result, err)
					if keepGoingFlag {
						report.cutWarnings()
						tests, _ := runAllTests(ctx.Context, result, true)
						report.setTestResults(tests)
					}
				}
				return err
			}
			if err := report.complete(ctx, result); err != nil {
				return err
			}
			report.cutWarnings()
			tests, err := runAllTests(ctx.Context, result, false)
			report.setTestResults(tests)
			if errors.Is(err, errNoTestTargets) && !requireTestsFlag {
				return nil
			}
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

func validateTestProject(ctx *RuntimeContext) error {
	dependedOn := make(map[string]bool)
	for _, node := range ctx.DepGraph.Packages {
		if node == nil {
			continue
		}
		for _, dep := range node.Deps {
			dependedOn[dep] = true
		}
	}
	roots := make([]string, 0)
	for name, node := range ctx.DepGraph.Packages {
		if node != nil && node.IsLocal() && !dependedOn[name] {
			roots = append(roots, name)
		}
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		platform, err := pipeline.ProjectPlatform(ctx)
		if err != nil {
			return err
		}
		return validateTestExecution(platform, runtime.GOOS)
	}
	var firstErr error
	for _, name := range roots {
		platform, err := pipeline.PackagePlatform(ctx, name)
		if err != nil {
			return err
		}
		if err := validateTestExecution(platform, runtime.GOOS); err == nil {
			return nil
		} else if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type testResult struct {
	fullName    string
	packageName string
	targetName  string
	outputPath  string
	passed      bool
	notRun      bool
	skipReason  string
	elapsed     time.Duration
	exitCode    int
	signal      string
	output      string
}

const (
	buildTestOutputLimit  = 4096
	buildTestOutputMarker = "...[truncated]\n"

	testSkipBuildFailed   = "build failed"
	testSkipBinaryMissing = "binary missing"
	testSkipUnsupported   = "unsupported platform"
)

var errNoTestTargets = errors.New("no test targets found")

func boundedTestOutput(output string) string {
	output = strings.TrimSpace(output)
	if len(output) <= buildTestOutputLimit {
		return output
	}
	keep := buildTestOutputLimit - len(buildTestOutputMarker)
	return buildTestOutputMarker + output[len(output)-keep:]
}

func testExitCode(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var exitErr *osexec.ExitError
	if errors.As(err, &exitErr) {
		if signal, ok := testProcessSignal(exitErr); ok {
			return -1, signal
		}
		if code := exitErr.ExitCode(); code >= 0 {
			return code, ""
		}
	}
	return 1, ""
}

func runAllTests(ctx context.Context, result *BuildResult, allowNotRun bool) ([]testResult, error) {
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

		test := testResult{
			fullName:    fullName,
			packageName: node.PkgName,
			targetName:  node.Target.Name(),
		}
		pkgDirs, ok := result.PkgDirs[node.PkgName]
		if !ok {
			if allowNotRun {
				test.notRun, test.skipReason = true, testSkipBuildFailed
				tests = append(tests, test)
			}
			continue
		}
		platform, ok := result.PkgPlatforms[node.PkgName]
		if !ok {
			if allowNotRun {
				test.notRun, test.skipReason = true, testSkipUnsupported
				tests = append(tests, test)
				continue
			}
			return nil, fmt.Errorf("test target %s has no resolved package platform", fullName)
		}
		test.outputPath = filepath.Join(pkgDirs.BuildDir, api.TargetFilename(node.Target.Kind(), node.Target.Name(), platform.OSOrHost()))
		if err := validateTestExecution(platform, runtime.GOOS); err != nil {
			if allowNotRun {
				test.notRun, test.skipReason = true, testSkipUnsupported
				tests = append(tests, test)
				continue
			}
			return nil, err
		}
		if _, err := os.Stat(test.outputPath); err != nil {
			if allowNotRun {
				test.notRun, test.skipReason = true, testSkipBinaryMissing
				tests = append(tests, test)
				continue
			}
			vlog.Error("FAIL %s (binary not found: %s)", fullName, test.outputPath)
			return nil, fmt.Errorf("test binary missing: %w", err)
		}

		tests = append(tests, test)
	}

	if len(tests) == 0 {
		return nil, errNoTestTargets
	}

	runnable := 0
	for _, test := range tests {
		if !test.notRun {
			runnable++
		}
	}
	if runnable > 0 {
		vlog.Info("")
		vlog.Info("Running %d test(s)...", runnable)
	}

	for i := range tests {
		if tests[i].notRun {
			continue
		}
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
		t.exitCode, t.signal = testExitCode(runErr)
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

	if runnable > 0 {
		vlog.Info("")
	}
	passed, failed := 0, 0
	for _, test := range tests {
		if test.notRun {
			continue
		}
		if test.passed {
			passed++
		} else {
			failed++
		}
	}

	if failed > 0 {
		return tests, fmt.Errorf("%d test(s) failed", failed)
	}
	return tests, nil
}
