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

func TestParseGNUDiagnosticsLocatedCMakeWarningContinuation(t *testing.T) {
	details := []string{"located warning with detail:", "first detail line", "second detail line"}
	output := strings.Join([]string{
		"CMake Warning at CMakeLists.txt:3 (message):",
		"  " + details[0],
		"    " + details[1],
		"\t" + details[2],
		"",
		"-- Configuring done (0.1s)",
	}, "\n")
	diagnostics := parseGNUDiagnostics(output)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Severity != "warning" || diagnostic.File != "CMakeLists.txt" || diagnostic.Line != 3 {
		t.Fatalf("located cmake warning = %+v", diagnostic)
	}
	for _, detail := range details {
		if !strings.Contains(diagnostic.Message, detail) {
			t.Errorf("message missing detail %q: %q", detail, diagnostic.Message)
		}
	}
	if strings.Contains(diagnostic.Message, "Configuring done") {
		t.Errorf("message collected output past the blank line: %q", diagnostic.Message)
	}
}

func TestParseGNUDiagnosticsLocatedCMakeErrorContinuation(t *testing.T) {
	output := strings.Join([]string{
		"CMake Error at CMakeLists.txt:4 (message):",
		"  configure exploded:",
		"  missing foo dependency",
		"",
		"-- Configuring incomplete, errors occurred!",
	}, "\n")
	diagnostics := parseGNUDiagnostics(output)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Severity != "error" || diagnostic.File != "CMakeLists.txt" || diagnostic.Line != 4 {
		t.Fatalf("located cmake error = %+v", diagnostic)
	}
	if !strings.Contains(diagnostic.Message, "configure exploded:") || !strings.Contains(diagnostic.Message, "missing foo dependency") {
		t.Fatalf("located cmake error detail = %q", diagnostic.Message)
	}
}

func TestParseGNUDiagnosticsCMakeWarningVariants(t *testing.T) {
	for _, header := range []string{
		"CMake Deprecation Warning at CMakeLists.txt:1 (cmake_minimum_required):",
		"CMake Dev Warning at cmake/checks.cmake:7 (message):",
		"CMake Author Warning at CMakeLists.txt:9 (message):",
		"CMake Warning (dev) at CMakeLists.txt:11 (message):",
	} {
		t.Run(header, func(t *testing.T) {
			output := strings.Join([]string{
				header,
				"  detailed warning text",
				"    with a second line",
				"",
				"src/unrelated.c:1:2: error: unrelated located error",
			}, "\n")
			diagnostics := parseGNUDiagnostics(output)
			if len(diagnostics) != 2 {
				t.Fatalf("diagnostics = %+v", diagnostics)
			}
			warning := diagnostics[0]
			if warning.Severity != "warning" || warning.File == "" || warning.Line == 0 {
				t.Fatalf("cmake warning variant = %+v", warning)
			}
			if !strings.Contains(warning.Message, "detailed warning text") || !strings.Contains(warning.Message, "with a second line") {
				t.Fatalf("cmake warning detail = %q", warning.Message)
			}
			if diagnostics[1].Severity != "error" || diagnostics[1].File != "src/unrelated.c" {
				t.Fatalf("diagnostic after cmake warning = %+v", diagnostics[1])
			}
		})
	}
}

func TestParseGNUDiagnosticsLocatedCMakeWarningContinuationCapped(t *testing.T) {
	line := strings.Repeat("x", buildDiagnosticMessageLimit)
	output := strings.Join([]string{
		"CMake Deprecation Warning at CMakeLists.txt:1 (cmake_minimum_required):",
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
	if strings.Contains(diagnostics[0].Message, line+line) {
		t.Fatal("message is not bounded")
	}
}

func TestParseCMakeDiagnosticDetailsAcrossBlankLine(t *testing.T) {
	output := strings.Join([]string{
		"CMake Error at CMakeLists.txt:4 (message):",
		"  fatal configure failure:",
		"",
		"    detail alpha",
		"    detail beta",
		"",
		"-- Configuring incomplete, errors occurred!",
	}, "\n")
	diagnostics := parseGNUDiagnostics(output)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	message := diagnostics[0].Message
	if !strings.Contains(message, "fatal configure failure") || !strings.Contains(message, "detail alpha") || !strings.Contains(message, "detail beta") {
		t.Fatalf("message = %q", message)
	}
	if strings.Contains(message, "Configuring incomplete") {
		t.Fatalf("message collected trailing output: %q", message)
	}
}
