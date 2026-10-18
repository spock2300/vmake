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

func TestTestCommandHelpDescribesContract(t *testing.T) {
	for _, want := range []string{
		"native test binaries",
		"target platform is checked",
		"vmake build --tests",
		"-k/--keep-going",
		"Partial mode runs",
		"notRun",
		"SKIP <pkg:target> (<reason>)",
		"no runnable test targets succeeds and exits 0",
		"--require-tests",
		"build/vmake-test.json",
		"signal",
		"exit",
	} {
		if !strings.Contains(testCmd.Long, want) {
			t.Errorf("test --help missing %q:\n%s", want, testCmd.Long)
		}
	}

	for name, want := range map[string][]string{
		"keep-going":    {"keep building independent targets after a failure", "built by this invocation", "not-run/stale"},
		"require-tests": {"no runnable test targets", "exit code 1", "exit 0"},
	} {
		flag := testCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("vmake test lost its --%s flag", name)
		}
		for _, phrase := range want {
			if !strings.Contains(flag.Usage, phrase) {
				t.Errorf("--%s help %q missing %q", name, flag.Usage, phrase)
			}
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
