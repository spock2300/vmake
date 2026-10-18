package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const passingTestSource = "int main(void) { return 0; }\n"

const failingTestSource = "int main(void) { return 1; }\n"

func writeTestCommandProject(t *testing.T, testSource string) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "tests-project")
	targets := `		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")`
	if testSource != "" {
		targets += `
		ctx.Target("tests").SetKind(api.TargetBinary).SetTest(true).AddFiles("tests/test_app.c")`
	}
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
`+targets+`
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	if testSource != "" {
		writeExtensionFile(t, filepath.Join(project, "tests", "test_app.c"), testSource)
	}
	return project
}

func TestTestCommandPublishesSucceededReport(t *testing.T) {
	project := writeTestCommandProject(t, passingTestSource)
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 0 || stderr != "" {
		t.Fatalf("test exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "1/1 test(s) passed.") || !strings.Contains(stdout, "Tests succeeded: 1/1 passed in ") {
		t.Fatalf("test summary missing:\n%s", stdout)
	}
	report := loadBuildReport(t, project)
	if report.SchemaVersion != 2 || report.Status != "succeeded" || report.Command != reportCommandTest || report.Error != "" {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Tests) != 1 {
		t.Fatalf("tests = %+v", report.Tests)
	}
	test := report.Tests[0]
	if test.Name != "tests-project:tests" || test.Package != "tests-project" || test.Target != "tests" ||
		!test.Passed || test.ElapsedMs < 0 || test.ExitCode != 0 {
		t.Fatalf("test entry = %+v", test)
	}
	if report.Summary == nil || report.Summary.TestsFound != 1 || report.Summary.TestsPassed != 1 || report.Summary.TestsFailed != 0 {
		t.Fatalf("summary counts = %+v", report.Summary)
	}
	if len(report.Targets) == 0 || report.CompileDatabase == "" || len(report.Artifacts) != 0 {
		t.Fatalf("test report fields = %+v", report)
	}
	if _, err := os.Stat(filepath.Join(project, "install")); !os.IsNotExist(err) {
		t.Fatalf("vmake test installed outputs: %v", err)
	}
}

func TestTestCommandFailureOverwritesSucceededReport(t *testing.T) {
	project := writeTestCommandProject(t, passingTestSource)
	if _, stderr, code := runBuildCommand(t, project, "build"); code != 0 {
		t.Fatalf("setup build exit = %d, stderr = %q", code, stderr)
	}
	previous := loadBuildReport(t, project)
	if previous.Status != "succeeded" || previous.Command != reportCommandBuild {
		t.Fatalf("setup report = %+v", previous)
	}
	writeExtensionFile(t, filepath.Join(project, "tests", "test_app.c"), failingTestSource)
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 1 {
		t.Fatalf("test exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "0/1 test(s) passed, 1 failed.") || !strings.Contains(stdout, "Tests failed: 1 test(s) failed") {
		t.Fatalf("failure summary missing:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Error: 1 test(s) failed") {
		t.Fatalf("failure error missing:\n%s", stderr)
	}
	report := loadBuildReport(t, project)
	if report.Status != "failed" || report.Command != reportCommandTest || report.BuildID == previous.BuildID {
		t.Fatalf("failed report = %+v", report)
	}
	if len(report.Tests) != 1 || report.Tests[0].Passed || report.Tests[0].Name != "tests-project:tests" {
		t.Fatalf("failed test entries = %+v", report.Tests)
	}
	if report.Summary == nil || report.Summary.TestsFound != 1 || report.Summary.TestsPassed != 0 || report.Summary.TestsFailed != 1 {
		t.Fatalf("failed summary = %+v", report.Summary)
	}
	if report.CompileDatabase == "" || len(report.Artifacts) != 0 {
		t.Fatalf("failed test report fields = %+v", report)
	}
}

func TestTestCommandNoTargetsFails(t *testing.T) {
	project := writeTestCommandProject(t, "")
	if stdout, stderr, code := runBuildCommand(t, project, "build", "--tests"); code != 0 {
		t.Fatalf("build --tests exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	previous := loadBuildReport(t, project)
	if previous.Status != "succeeded" || previous.Command != reportCommandBuild || previous.Summary == nil || previous.Summary.TestsFound != 0 {
		t.Fatalf("build --tests should stay a successful build with zero tests: %+v", previous)
	}
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 1 {
		t.Fatalf("test exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Tests failed: no test targets found") {
		t.Fatalf("no-tests stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "Error: no test targets found") {
		t.Fatalf("no-tests stderr = %q", stderr)
	}
	report := loadBuildReport(t, project)
	if report.Status != "failed" || report.Command != reportCommandTest ||
		report.Error != "no test targets found" || report.BuildID == previous.BuildID {
		t.Fatalf("no-tests report = %+v", report)
	}
	if report.Tests == nil || len(report.Tests) != 0 {
		t.Fatalf("no-tests entries = %#v, want []", report.Tests)
	}
	if report.Summary == nil || report.Summary.TestsFound != 0 || report.Summary.TestsPassed != 0 || report.Summary.TestsFailed != 0 {
		t.Fatalf("no-tests summary = %+v", report.Summary)
	}
}

func TestTestCommandBuildFailurePublishesReport(t *testing.T) {
	project := writeTestCommandProject(t, passingTestSource)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) {\n\tint value = ;\n\treturn value;\n}\n")
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 1 {
		t.Fatalf("test exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Tests failed: src/main.c:2:") {
		t.Fatalf("build-failure summary missing:\n%s", stdout)
	}
	report := loadBuildReport(t, project)
	if report.Status != "failed" || report.Command != reportCommandTest || report.BuildID == "" {
		t.Fatalf("build-failure report = %+v", report)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "VMAKE_E_COMPILE" {
		t.Fatalf("build-failure diagnostics = %+v", report.Diagnostics)
	}
	if report.Toolchain == "" || report.Mode == "" || len(report.Global) == 0 {
		t.Fatalf("build-failure identity = %+v (toolchain %q, mode %q)", report.Global, report.Toolchain, report.Mode)
	}
	if report.CompileDatabase == "" {
		t.Fatal("build-failure report lost the compile database")
	}
	if report.Tests == nil || len(report.Tests) != 0 || report.Summary == nil {
		t.Fatalf("build-failure results = %+v (summary %+v)", report.Tests, report.Summary)
	}
	if report.Summary.TargetsFailed != 1 || report.Summary.TargetsBuilt != 0 || report.Summary.TestsFound != 0 ||
		report.Summary.ArtifactsInstalled != 0 {
		t.Fatalf("build-failure summary = %+v", report.Summary)
	}
}
