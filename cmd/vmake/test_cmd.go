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
	"github.com/spock2300/vmake/internal/gosrc"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
)

var requireTestsFlag bool

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Build and run test targets",
	Long: `Build the project's test targets and run their native test binaries.

Test targets are default binary targets declared with SetTest(true). vmake
first checks whether any runnable test target is declared; when none is, it
skips the build and succeeds (or fails with --require-tests). When tests exist,
the target platform is checked: vmake test refuses to execute tests for cross,
bare-metal, or triple-configured targets and points at 'vmake build --tests',
which builds them without running them.

With -k/--keep-going a failed build does not abort testing. Partial mode runs
only the test targets built by this invocation; test targets that were not
built, or whose binary is missing or stale, are recorded as notRun with a
skipReason and printed as SKIP <pkg:target> (<reason>) at default verbosity.
Quiet mode (-q) suppresses those lines.

A project with no runnable test targets succeeds and exits 0 without running
the build phase: test declares targets cheaply first and skips the build when
none is a test target, publishing a succeeded test report with testsFound 0.
Pass --require-tests to make that case fail before the build with
VMAKE_E_TEST. A declaration that cannot be trusted (missing sources or a
script error) falls back to the normal build-then-discover path.

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
			if tests, known := countRunnableTestTargets(ctx); known && tests == 0 {
				report.Summary = newBuildSummary(&BuildResult{}, 0)
				if requireTestsFlag {
					return errNoTestTargets
				}
				report.completed = true
				return nil
			}
			if err := validateTestProject(ctx); err != nil {
				return err
			}
			result, err := runBuildPhase(ctx, BuildOptions{IncludeTests: true, Jobs: jobsFlag, KeepGoing: keepGoingFlag, Explain: explainFlag})
			if err != nil {
				if result != nil {
					report.partial(ctx, result, err)
					if keepGoingFlag && ctx.Context.Err() == nil {
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

// countRunnableTestTargets cheaply declares targets to find the default binary
// test targets runAllTests would run. It reports false when declaration cannot
// be trusted, so the caller falls back to the normal build path.
func countRunnableTestTargets(ctx *RuntimeContext) (int, bool) {
	restoreStdout := silenceInspectStdout()
	restoreStderr := silenceInspectStderr()
	defer func() {
		restoreStdout()
		restoreStderr()
	}()
	scriptfs.SetReadOnly(true)
	defer scriptfs.SetReadOnly(false)

	inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
	if err != nil || len(inspection.Needed) == 0 {
		return 0, false
	}
	scriptfs.TakeBlockedWrites()
	count := 0
	blocked := false
	for _, name := range ctx.Resolver.GetOrder() {
		if !inspection.Needed[name] {
			continue
		}
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil || !inspectSourcePresent(ctx, name, node) {
			return 0, false
		}
		dirs := inspection.PkgDirs[name]
		if dirs == nil || dirs.BuildDir == "" {
			return 0, false
		}
		buildCtx, err := pipeline.DeclareTargets(ctx, name, dirs, inspection.PackageToolchains[name], inspection.GlobalValues)
		if blocked := scriptfs.TakeBlockedWrites(); len(blocked) > 0 && err == nil {
			err = errors.New(blocked[0])
		}
		if err != nil {
			if strings.Contains(err.Error(), "read-only inspection:") {
				blocked = true
				continue
			}
			return 0, false
		}
		for _, target := range buildCtx.GetTargets() {
			if target.IsTest() && target.IsDefault() && target.Kind() == api.TargetBinary {
				count++
			}
		}
	}
	if blocked && count == 0 && neededPackagesDeclareTests(ctx, inspection) {
		return 0, false
	}
	return count, true
}

func neededPackagesDeclareTests(ctx *RuntimeContext, inspection *pipeline.Inspection) bool {
	for name := range inspection.Needed {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil || node.Source.Dir == "" {
			return true
		}
		files, err := gosrc.ListGoFiles(node.Source.Dir)
		if err != nil || len(files) == 0 {
			return true
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				return true
			}
			if strings.Contains(string(data), "SetTest(") {
				return true
			}
		}
	}
	return false
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

	testSkipBuildFailed    = "build failed"
	testSkipTargetNotBuilt = "target not built"
	testSkipBinaryMissing  = "binary missing"
	testSkipUnsupported    = "unsupported platform"
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

func runAllTests(ctx context.Context, result *BuildResult, allowNotRun bool) ([]testResult, error) {
	var tests []testResult

	builtTargets := make(map[string]bool, len(result.Targets))
	for _, target := range result.Targets {
		if target.Status != build.TargetStatusBuilt && target.Status != build.TargetStatusUpToDate {
			continue
		}
		builtTargets[target.Package+"\x00"+target.Target] = true
	}

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
		if allowNotRun && !builtTargets[node.PkgName+"\x00"+node.Target.Name()] {
			test.notRun, test.skipReason = true, testSkipTargetNotBuilt
			tests = append(tests, test)
			continue
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

	if runnable > 0 {
		vlog.Info("")
	}
	skipped := 0
	for _, test := range tests {
		if !test.notRun {
			continue
		}
		skipped++
		vlog.Info("SKIP %s (%s)", test.fullName, test.skipReason)
	}
	if skipped > 0 {
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
