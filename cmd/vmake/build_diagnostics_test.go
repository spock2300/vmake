package main

import (
	"strings"
	"testing"
)

func TestParseGNUDiagnosticsCompilerAndLinkerLines(t *testing.T) {
	output := strings.Join([]string{
		"src/main.c:4:9: warning: unused variable 'x'",
		"src/main.c:8:2: fatal error: missing.h: No such file or directory",
		"src/other.c:12: undefined reference to `foo'",
		"include/api.h:3:1: note: declared here",
	}, "\n")
	diagnostics := parseGNUDiagnostics(output)
	if len(diagnostics) != 4 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if diagnostics[0].Severity != "warning" || diagnostics[0].File != "src/main.c" || diagnostics[0].Line != 4 || diagnostics[0].Column != 9 {
		t.Fatalf("diagnostics[0] = %+v", diagnostics[0])
	}
	if diagnostics[1].Severity != "error" || diagnostics[1].File != "src/main.c" || diagnostics[1].Line != 8 || diagnostics[1].Column != 2 {
		t.Fatalf("diagnostics[1] = %+v", diagnostics[1])
	}
	if diagnostics[2].Severity != "error" || diagnostics[2].File != "src/other.c" || diagnostics[2].Line != 12 {
		t.Fatalf("diagnostics[2] = %+v", diagnostics[2])
	}
	if diagnostics[3].Severity != "note" || diagnostics[3].File != "include/api.h" || diagnostics[3].Line != 3 {
		t.Fatalf("diagnostics[3] = %+v", diagnostics[3])
	}
}
