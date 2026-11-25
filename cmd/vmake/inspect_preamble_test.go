package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writePreambleProject writes a project whose OnBuild body is onBuild. The
// target is a void callback so inspection never needs compilers.
func writePreambleProject(t *testing.T, onBuild string) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import (
	"fmt"
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
`+onBuild+`
		ctx.Target("app").SetKind(api.TargetVoid)
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	return project
}

// TestInspectionQuietPolicy pins the preamble's quiet rules: machine output
// (inspect) keeps stdout pure, a human command (query) still shows in-process
// prints, and a child process' output is silenced in both. The fixture forces
// dry-run off so the child really runs; the marker file proves it, which keeps
// the assertion from passing vacuously.
func TestInspectionQuietPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture runs a POSIX shell")
	}
	marker := filepath.Join(t.TempDir(), "child-ran")
	project := writePreambleProject(t, `		fmt.Println("IN-PROCESS-PRINT")
		p.SetDryRun(false)
		p.RunIn(p.SourceDir(), "sh", "-c", "echo ran > `+filepath.ToSlash(marker)+`; echo CHILD-PROCESS-PRINT")`)

	assertChildRan := func(t *testing.T) {
		t.Helper()
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("child process did not run: %v", err)
		}
		os.Remove(marker)
	}

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit=%d stderr=%q", code, stderr)
	}
	assertChildRan(t)
	var envelope struct {
		SchemaVersion int    `json:"schemaVersion"`
		Command       string `json:"command"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("inspect stdout is not pure JSON: %v\n%s", err, stdout)
	}
	if envelope.Command != "inspect" {
		t.Fatalf("inspect command = %q", envelope.Command)
	}
	for _, leaked := range []string{"IN-PROCESS-PRINT", "CHILD-PROCESS-PRINT"} {
		if strings.Contains(stdout, leaked) {
			t.Fatalf("inspect stdout leaked %s:\n%s", leaked, stdout)
		}
	}

	stdout, stderr, code = runInspectCommand(t, project, "query", "targets")
	if code != 0 {
		t.Fatalf("query exit=%d stderr=%q", code, stderr)
	}
	assertChildRan(t)
	if !strings.Contains(stdout, "IN-PROCESS-PRINT") {
		t.Fatalf("query hid the buildscript's in-process print:\n%s", stdout)
	}
	if strings.Contains(stdout, "CHILD-PROCESS-PRINT") {
		t.Fatalf("query leaked child-process output:\n%s", stdout)
	}
}

// TestInspectionBlockedWriteReported pins how each command reports a write the
// read-only inspection scope refused.
func TestInspectionBlockedWriteReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture relies on POSIX paths")
	}
	project := writePreambleProject(t, `		os.WriteFile("blocked.txt", []byte("x"), 0644)`)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit=%d stderr=%q", code, stderr)
	}
	var envelope struct {
		Diagnostics []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("unmarshal inspect: %v\n%s", err, stdout)
	}
	blocked := 0
	for _, diagnostic := range envelope.Diagnostics {
		if strings.Contains(diagnostic.Message, "read-only inspection") {
			blocked++
		}
	}
	if blocked == 0 {
		t.Fatalf("inspect did not report the blocked write: %+v", envelope.Diagnostics)
	}
	if _, err := os.Stat(filepath.Join(project, "blocked.txt")); !os.IsNotExist(err) {
		t.Fatalf("inspection created the blocked file: %v", err)
	}

	stdout, stderr, code = runInspectCommand(t, project, "check-symbols")
	if code != 0 {
		t.Fatalf("check-symbols exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "[blocked-write]") {
		t.Fatalf("check-symbols did not report the blocked write:\n%s", stdout)
	}
}

// TestInspectionStageFailureExitCodes pins the stage-failure mapping each
// command applies to the shared preamble.
func TestInspectionStageFailureExitCodes(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		panic("config exploded")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetVoid)
	})
}
`)
	for _, args := range [][]string{
		{"inspect"},
		{"query", "targets"},
		{"check-symbols"},
		{"deps", "graph"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr, code := runInspectCommand(t, project, args...)
			if code != 1 {
				t.Fatalf("exit=%d, want 1 (stderr %q)", code, stderr)
			}
			if !strings.Contains(stderr, "config exploded") {
				t.Fatalf("stderr = %q, want the config panic", stderr)
			}
			if args[0] == "inspect" {
				var envelope struct {
					Status      string `json:"status"`
					Diagnostics []struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"diagnostics"`
				}
				if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
					t.Fatalf("inspect envelope is not JSON: %v\n%s", err, stdout)
				}
				if envelope.Status != "error" || len(envelope.Diagnostics) == 0 {
					t.Fatalf("envelope = %+v, want status error with a diagnostic", envelope)
				}
				if envelope.Diagnostics[0].Code != "VMAKE_E_INSPECT" {
					t.Fatalf("diagnostic code = %q", envelope.Diagnostics[0].Code)
				}
			}
		})
	}
}
