package main

import (
	"strings"
	"testing"
)

func TestTestCommandTestsFlagIsDocumentedNoOp(t *testing.T) {
	flag := testCmd.Flags().Lookup("tests")
	if flag == nil {
		t.Fatal("vmake test lost its --tests flag")
	}
	for _, want := range []string{"implied", "no effect", "always builds and runs test targets"} {
		if !strings.Contains(flag.Usage, want) {
			t.Errorf("--tests help %q missing %q", flag.Usage, want)
		}
	}
}

func TestTestCommandAcceptsTestsFlag(t *testing.T) {
	project := writeTestCommandProject(t, passingTestSource)
	stdout, stderr, code := runBuildCommand(t, project, "test", "--tests")
	if code != 0 || stderr != "" {
		t.Fatalf("test --tests exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Tests succeeded: 1/1 passed") {
		t.Fatalf("test --tests summary missing:\n%s", stdout)
	}
	report := loadTestReport(t, project)
	if report.Command != reportCommandTest || report.Status != "succeeded" || len(report.Tests) != 1 || !report.Tests[0].Passed {
		t.Fatalf("test --tests report = %+v", report)
	}
}
