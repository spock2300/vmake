package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildreport"
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

func runTest(cmd *cobra.Command, args []string) error {
	commandStorageLocks()
	return withReport(buildreport.CommandTest, func(report *buildreport.Report) error {
		execution := &RuntimeContext{}
		return withResolvedProject(execution, func(ctx *RuntimeContext) error {
			if err := report.Bind(ctx); err != nil {
				return err
			}
			if err := pipeline.ValidateTestProject(ctx); err != nil {
				return err
			}
			result, err := runBuildPhase(ctx, BuildOptions{IncludeTests: true, Jobs: jobsFlag, KeepGoing: keepGoingFlag, Explain: explainFlag})
			if err != nil {
				if result != nil {
					report.Partial(ctx, result, err)
				}
				return err
			}
			if err := report.Complete(ctx, result); err != nil {
				return err
			}
			tests, err := pipeline.RunAllTests(ctx.Context, result)
			report.SetTestResults(tests)
			if errors.Is(err, pipeline.ErrNoTestTargets) {
				return nil
			}
			return err
		})
	})
}

func validateTestExecution(platform api.Platform, hostOS string) error {
	return pipeline.ValidateTestExecution(platform, hostOS)
}

type testResult = pipeline.TestResult

const (
	buildTestOutputLimit  = pipeline.BuildTestOutputLimit
	buildTestOutputMarker = pipeline.BuildTestOutputMarker
)

func boundedTestOutput(output string) string {
	return pipeline.BoundedTestOutput(output)
}
