package main

import (
	"os"

	"github.com/spock2300/vmake/pkg/buildreport"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func withBuildReport(run func(*buildreport.Report) error) error {
	return withReport(buildreport.CommandBuild, run)
}

func withReport(command string, run func(*buildreport.Report) error) (err error) {
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
		if r := recover(); r != nil {
			// A fatal inside the command unwinds through here. Record the
			// failure before re-raising, otherwise the report would be
			// finished with a nil error and claim success.
			if _, ok := r.(*exitError); ok {
				reportErr := r.(error)
				report.Finish(&reportErr)
				restoreStdout()
				buildreport.PrintSummary(stdout, report, reportErr)
			}
			panic(r)
		}
		report.Finish(&err)
		restoreStdout()
		buildreport.PrintSummary(stdout, report, err)
		if err != nil && report.Error != "" {
			err = &buildreport.SummaryError{Summary: report.Error, Err: err}
		}
	}()
	return run(report)
}

func beginBuildReport(command string) (*buildreport.Report, error) {
	return buildreport.Begin(findProjectDir(), command, currentCommandSession().configSelector)
}

func invalidateBuildReport() error {
	return buildreport.InvalidateReports(findProjectDir())
}
