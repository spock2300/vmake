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
	"unicode/utf8"

	"github.com/spf13/cobra"

	exec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Build and run test targets",
	Long: `Build the project's test targets and run their native test binaries.

Test targets are default binary targets declared with SetTest(true). The target
platform is checked before building: vmake test refuses to execute tests for
cross, bare-metal, or triple-configured targets and points at
'vmake build --tests', which builds them without running them.

A project with no runnable test targets succeeds and exits 0, publishing a
succeeded test report with testsFound 0.

Results are published to build/vmake-test.json (schemaVersion 2) with per-test
pass/fail, exit code, signal, and output. A failing or signaled test exits 1;
interrupting vmake with SIGINT/SIGTERM exits 130/143.`,
	RunE: runTest,
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
				}
				return err
			}
			if err := report.complete(ctx, result); err != nil {
				return err
			}
			tests, err := runAllTests(ctx.Context, result)
			report.setTestResults(tests)
			if errors.Is(err, errNoTestTargets) {
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
	var explicitRoots []string
	dependedOn := make(map[string]bool)
	for name, node := range ctx.DepGraph.Packages {
		if node == nil {
			continue
		}
		if node.IsLocal() && node.Pkg != nil && node.Pkg.IsRoot() {
			explicitRoots = append(explicitRoots, name)
		}
		for _, dep := range node.Deps {
			dependedOn[dep] = true
		}
	}
	roots := explicitRoots
	if len(roots) == 0 {
		for name, node := range ctx.DepGraph.Packages {
			if node != nil && node.IsLocal() && !dependedOn[name] {
				roots = append(roots, name)
			}
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
)

var errNoTestTargets = errors.New("no test targets found")

func boundedTestOutput(output string) string {
	output = strings.TrimSpace(output)
	if len(output) <= buildTestOutputLimit {
		return output
	}
	keep := buildTestOutputLimit - len(buildTestOutputMarker)
	tail := output[len(output)-keep:]
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	return buildTestOutputMarker + tail
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
		outputPath := filepath.Join(pkgDirs.BuildDir, api.TargetFilename(node.Target.Kind(), node.Target.Name(), platform.OSOrHost()))
		if err := validateTestExecution(platform, runtime.GOOS); err != nil {
			return nil, err
		}
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
		return nil, errNoTestTargets
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
			t.elapsed = time.Since(start)
			t.notRun = true
			t.skipReason = "interrupted"
			tests = append(tests[:i], *t)
			return tests, ctx.Err()
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

	vlog.Info("")
	failed := 0
	for _, test := range tests {
		if !test.passed {
			failed++
		}
	}

	if failed > 0 {
		return tests, fmt.Errorf("%d test(s) failed", failed)
	}
	return tests, nil
}
