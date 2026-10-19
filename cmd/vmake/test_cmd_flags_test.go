package main

import (
	"strings"
	"testing"
)

func TestTestCommandHelpDescribesContract(t *testing.T) {
	for _, want := range []string{
		"native test binaries",
		"platform is checked before building",
		"vmake build --tests",
		"no runnable test targets succeeds and exits 0",
		"build/vmake-test.json",
		"signal",
		"exit",
	} {
		if !strings.Contains(testCmd.Long, want) {
			t.Errorf("test --help missing %q:\n%s", want, testCmd.Long)
		}
	}

	flag := testCmd.Flags().Lookup("keep-going")
	if flag == nil {
		t.Fatal("vmake test lost its --keep-going flag")
	}
	if !strings.Contains(flag.Usage, "keep building independent targets after a failure") {
		t.Errorf("--keep-going help %q missing expected description", flag.Usage)
	}
	for _, removed := range []string{"tests", "require-tests"} {
		if testCmd.Flags().Lookup(removed) != nil {
			t.Errorf("vmake test still registers removed --%s flag", removed)
		}
	}
}
