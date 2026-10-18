package main

import (
	"strings"
	"testing"
)

func TestParseGNUDiagnosticsBareCMakeErrorContinuation(t *testing.T) {
	variables := []string{"FIRST_MISSING_VARIABLE", "SECOND_MISSING_VARIABLE"}
	lines := []string{
		"CMake Error:",
		"  Could not find the required variable:",
	}
	for _, variable := range variables {
		lines = append(lines, "    "+variable)
	}
	lines = append(lines, "", "    cmake --build", "-- Configuring incomplete, errors occurred!")
	diagnostics := parseGNUDiagnostics(strings.Join(lines, "\n"))
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Severity != "error" || diagnostic.File != "" || diagnostic.Line != 0 {
		t.Fatalf("bare cmake error = %+v", diagnostic)
	}
	for _, variable := range variables {
		if !strings.Contains(diagnostic.Message, variable) {
			t.Errorf("message missing continuation %q: %q", variable, diagnostic.Message)
		}
	}
	if strings.Contains(diagnostic.Message, "cmake --build") || strings.Contains(diagnostic.Message, "Configuring incomplete") {
		t.Errorf("message collected output past the blank line: %q", diagnostic.Message)
	}
}

func TestParseGNUDiagnosticsBareCMakeErrorContinuationCapped(t *testing.T) {
	line := strings.Repeat("x", buildDiagnosticMessageLimit)
	output := strings.Join([]string{
		"CMake Error:",
		"  " + line,
		"  " + line,
		"  " + line,
		"",
	}, "\n")
	diagnostics := parseGNUDiagnostics(output)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if got := len([]rune(diagnostics[0].Message)); got != buildDiagnosticMessageLimit {
		t.Fatalf("message length = %d, want %d", got, buildDiagnosticMessageLimit)
	}
}
