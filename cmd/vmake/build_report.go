package main

import (
	"io"
	"os"

	"github.com/spock2300/vmake/pkg/buildreport"
	vlog "github.com/spock2300/vmake/pkg/log"
)

const (
	reportCommandBuild   = buildreport.CommandBuild
	reportCommandRebuild = buildreport.CommandRebuild
	reportCommandTest    = buildreport.CommandTest

	buildReportFile = buildreport.BuildReportFile
	testReportFile  = buildreport.TestReportFile
)

type (
	buildArtifact     = buildreport.Artifact
	buildSummary      = buildreport.Summary
	buildTestResult   = buildreport.TestResult
	buildReport       = buildreport.Report
	buildSummaryError = buildreport.SummaryError
)

func withBuildReport(run func(*buildReport) error) error {
	return withReport(reportCommandBuild, run)
}

func withReport(command string, run func(*buildReport) error) (err error) {
	report, err := beginBuildReport(command)
	if err != nil {
		return err
	}
	stdout := os.Stdout
	restoreStdout := func() {}
	if vlog.IsQuiet() {
		if devNull, openErr := os.OpenFile(os.DevNull, os.O_WRONLY, 0); openErr == nil {
			os.Stdout = devNull
			restored := false
			restoreStdout = func() {
				if restored {
					return
				}
				restored = true
				os.Stdout = stdout
				devNull.Close()
			}
		}
	}
	defer func() {
		defer restoreStdout()
		report.Finish(&err)
		restoreStdout()
		printBuildSummary(stdout, report, err)
		if err != nil && report.Error != "" {
			err = &buildSummaryError{Summary: report.Error, Err: err}
		}
	}()
	return run(report)
}

func printBuildSummary(writer io.Writer, report *buildReport, err error) {
	buildreport.PrintSummary(writer, report, err)
}

func printTestSummary(writer io.Writer, report *buildReport, err error) {
	buildreport.PrintTestSummary(writer, report, err)
}

func formatBuildDuration(durationMs int64) string {
	return buildreport.FormatDuration(durationMs)
}

func reportPath(root string) string     { return buildreport.ReportPath(root) }
func testReportPath(root string) string { return buildreport.TestReportPath(root) }
func reportPathFor(root, command string) string {
	return buildreport.ReportPathFor(root, command)
}

func beginBuildReport(command string) (*buildReport, error) {
	return buildreport.Begin(findProjectDir(), command, currentCommandSession().configSelector)
}

func invalidateBuildReport() error {
	return buildreport.InvalidateReports(findProjectDir())
}

func fileDigest(path string) (string, int64, error) {
	return buildreport.FileDigest(path)
}
