package main

import (
	"os"

	quietpkg "github.com/spock2300/vmake/internal/quiet"
	"github.com/spock2300/vmake/pkg/buildreport"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func withBuildReport(run func(*buildreport.Report) error) error {
	return withReport(buildreport.CommandBuild, run)
}

func withReport(command string, run func(*buildreport.Report) error) (err error) {
	report, err := buildreport.Begin(findProjectDir(), command, currentCommandSession().configSelector)
	if err != nil {
		return err
	}
	stdout := os.Stdout
	restoreStdout := func() {}
	if vlog.IsQuiet() {
		restoreStdout = quietpkg.SilenceStdout()
	}
	defer func() {
		defer restoreStdout()
		if r := recover(); r != nil {
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
