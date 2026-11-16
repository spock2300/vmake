package main

import "github.com/spock2300/vmake/pkg/buildreport"

const (
	buildDiagnosticMessageLimit = buildreport.DiagnosticMessageLimit
	buildSummaryLimit           = buildreport.SummaryLimit
)

type buildDiagnostic = buildreport.Diagnostic

func buildFailureDetails(err error) ([]buildDiagnostic, string) {
	return buildreport.FailureDetails(err)
}

func parseGNUDiagnostics(output string) []buildDiagnostic {
	return buildreport.ParseGNUDiagnostics(output)
}
