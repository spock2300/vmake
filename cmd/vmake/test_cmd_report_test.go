package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spock2300/vmake/internal/jsonio"
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

func writeBareMetalTestProject(t *testing.T) string {
	t.Helper()
	project := writeTestCommandProject(t, passingTestSource)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","global":{"options":{"target_os":"none","target_triple":"arm-none-eabi"}}}`)
	return project
}

func loadTestReport(t *testing.T, project string) buildReport {
	t.Helper()
	var report buildReport
	if err := jsonio.Load(testReportPath(project), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

type reportSnapshot struct {
	data    []byte
	modTime time.Time
}

func snapshotReport(t *testing.T, path string) reportSnapshot {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return reportSnapshot{data: data, modTime: info.ModTime()}
}

func assertReportUnchanged(t *testing.T, path string, before reportSnapshot) {
	t.Helper()
	after := snapshotReport(t, path)
	if !bytes.Equal(before.data, after.data) {
		t.Fatalf("%s was rewritten:\nbefore: %s\nafter:  %s", path, before.data, after.data)
	}
	if !before.modTime.Equal(after.modTime) {
		t.Fatalf("%s mtime changed: %v -> %v", path, before.modTime, after.modTime)
	}
}

func buildAndSnapshotBuildReport(t *testing.T, project string) reportSnapshot {
	t.Helper()
	if stdout, stderr, code := runBuildCommand(t, project, "build"); code != 0 {
		t.Fatalf("setup build exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	return snapshotReport(t, reportPath(project))
}

func TestTestCommandPublishesSucceededTestReport(t *testing.T) {
	project := writeTestCommandProject(t, passingTestSource)
	buildReportSnapshot := buildAndSnapshotBuildReport(t, project)
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 0 || stderr != "" {
		t.Fatalf("test exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Tests succeeded: 1/1 passed in ") {
		t.Fatalf("test summary missing:\n%s", stdout)
	}
	assertReportUnchanged(t, reportPath(project), buildReportSnapshot)
	report := loadTestReport(t, project)
	if report.SchemaVersion != 2 || report.Status != "succeeded" || report.Command != reportCommandTest || report.Error != "" {
		t.Fatalf("test report = %+v", report)
	}
	if report.BuildID == "" || report.Root != project || report.DurationMs <= 0 {
		t.Fatalf("test report identity = %+v", report)
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

func TestTestCommandFailurePreservesBuildReport(t *testing.T) {
	project := writeTestCommandProject(t, passingTestSource)
	buildReportSnapshot := buildAndSnapshotBuildReport(t, project)
	writeExtensionFile(t, filepath.Join(project, "tests", "test_app.c"), failingTestSource)
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 1 {
		t.Fatalf("test exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Tests failed: 1 test(s) failed") {
		t.Fatalf("failure summary missing:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Error: 1 test(s) failed") {
		t.Fatalf("failure error missing:\n%s", stderr)
	}
	assertReportUnchanged(t, reportPath(project), buildReportSnapshot)
	report := loadTestReport(t, project)
	if report.Status != "failed" || report.Command != reportCommandTest || report.BuildID == "" {
		t.Fatalf("failed test report = %+v", report)
	}
	if len(report.Tests) != 1 || report.Tests[0].Passed || report.Tests[0].Name != "tests-project:tests" {
		t.Fatalf("failed test entries = %+v", report.Tests)
	}
	if report.Summary == nil || report.Summary.TestsFound != 1 || report.Summary.TestsPassed != 0 || report.Summary.TestsFailed != 1 {
		t.Fatalf("failed summary = %+v", report.Summary)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Severity != "error" ||
		report.Diagnostics[0].Code != "VMAKE_E_TEST" || report.Diagnostics[0].Message != "1 test(s) failed" {
		t.Fatalf("failed test diagnostics = %+v", report.Diagnostics)
	}
	if report.CompileDatabase == "" || len(report.Artifacts) != 0 {
		t.Fatalf("failed test report fields = %+v", report)
	}
}

func TestTestCommandNoTargetsSucceeds(t *testing.T) {
	project := writeTestCommandProject(t, "")
	buildReportSnapshot := buildAndSnapshotBuildReport(t, project)
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 0 {
		t.Fatalf("test exit = %d, want 0 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Tests succeeded: no test targets") {
		t.Fatalf("no-tests summary missing:\n%s", stdout)
	}
	if stderr != "" {
		t.Fatalf("no-tests stderr = %q", stderr)
	}
	assertReportUnchanged(t, reportPath(project), buildReportSnapshot)
	report := loadTestReport(t, project)
	if report.Status != "succeeded" || report.Command != reportCommandTest || report.Error != "" {
		t.Fatalf("no-tests report = %+v", report)
	}
	if report.Tests == nil || len(report.Tests) != 0 {
		t.Fatalf("no-tests entries = %#v, want []", report.Tests)
	}
	if report.Summary == nil || report.Summary.TestsFound != 0 || report.Summary.TestsPassed != 0 || report.Summary.TestsFailed != 0 {
		t.Fatalf("no-tests summary = %+v", report.Summary)
	}
}

func TestTestCommandBareMetalPreflight(t *testing.T) {
	project := writeBareMetalTestProject(t)
	writeExtensionFile(t, reportPath(project), `{"schemaVersion":2,"buildId":"previous-build","status":"succeeded","command":"build"}`)
	buildReportSnapshot := snapshotReport(t, reportPath(project))
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 1 {
		t.Fatalf("bare-metal test exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `cannot run tests for project target (target_os=none, target_triple="arm-none-eabi")`) {
		t.Fatalf("preflight message missing from stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "use 'vmake build --tests'") {
		t.Fatalf("preflight hint missing from stderr:\n%s", stderr)
	}
	if strings.Contains(stdout, "Building...") || strings.Contains(stdout, "Executing OnBuild") || strings.Contains(stdout, "  CC ") {
		t.Fatalf("preflight ran the build:\n%s", stdout)
	}
	if dirs := buildKeyDirectories(t, project); len(dirs) != 0 {
		t.Fatalf("preflight created build-key directories: %v", dirs)
	}
	if _, err := os.Stat(filepath.Join(project, "build", "compile_commands.json")); !os.IsNotExist(err) {
		t.Fatalf("preflight created the compile database: %v", err)
	}
	assertReportUnchanged(t, reportPath(project), buildReportSnapshot)
	report := loadTestReport(t, project)
	if report.Status != "failed" || report.Command != reportCommandTest {
		t.Fatalf("preflight report = %+v", report)
	}
	if !strings.Contains(report.Error, "use 'vmake build --tests'") || report.Summary != nil {
		t.Fatalf("preflight error/summary = %q / %+v", report.Error, report.Summary)
	}
	if report.Tests == nil || len(report.Tests) != 0 || len(report.Artifacts) != 0 {
		t.Fatalf("preflight results = %+v (artifacts %+v)", report.Tests, report.Artifacts)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Severity != "error" ||
		report.Diagnostics[0].Code != "VMAKE_E_TEST" || !strings.Contains(report.Diagnostics[0].Message, "use 'vmake build --tests'") {
		t.Fatalf("preflight diagnostics = %+v", report.Diagnostics)
	}
}

func TestTestCommandBuildFailurePreservesBuildReport(t *testing.T) {
	project := writeTestCommandProject(t, passingTestSource)
	buildReportSnapshot := buildAndSnapshotBuildReport(t, project)
	writeExtensionFile(t, filepath.Join(project, "tests", "test_app.c"), "int main(void) {\n\tint value = ;\n\treturn value;\n}\n")
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 1 {
		t.Fatalf("test exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Tests failed: tests/test_app.c:2:") {
		t.Fatalf("build-failure summary missing:\n%s", stdout)
	}
	assertReportUnchanged(t, reportPath(project), buildReportSnapshot)
	report := loadTestReport(t, project)
	if report.Status != "failed" || report.Command != reportCommandTest || report.BuildID == "" {
		t.Fatalf("build-failure report = %+v", report)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "VMAKE_E_COMPILE" {
		t.Fatalf("build-failure diagnostics = %+v", report.Diagnostics)
	}
	if report.Toolchain == "" || report.Mode == "" || len(report.Global) == 0 {
		t.Fatalf("build-failure identity = %+v (toolchain %q, mode %q)", report.Global, report.Toolchain, report.Mode)
	}
	if report.Tests == nil || len(report.Tests) != 0 || report.Summary == nil {
		t.Fatalf("build-failure results = %+v (summary %+v)", report.Tests, report.Summary)
	}
	if report.Summary.TargetsFailed != 1 || report.Summary.TargetsBuilt != 0 || report.Summary.TargetsUpToDate != 1 || report.Summary.TestsFound != 0 ||
		report.Summary.ArtifactsInstalled != 0 {
		t.Fatalf("build-failure summary = %+v", report.Summary)
	}
}

func TestTestCommandReportsSignalOutcome(t *testing.T) {
	if !testSignalsSupported {
		t.Skip("process signal status is unavailable on this platform")
	}
	project := writeTestCommandProject(t, `#include <signal.h>
#include <stdio.h>

int main(void) {
	fprintf(stderr, "crashing test marker\n");
	raise(SIGSEGV);
	return 0;
}
`)
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 1 {
		t.Fatalf("test exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "FAIL tests-project:tests") {
		t.Fatalf("FAIL line missing from stderr:\n%s", stderr)
	}
	report := loadTestReport(t, project)
	if len(report.Tests) != 1 {
		t.Fatalf("tests = %+v", report.Tests)
	}
	test := report.Tests[0]
	if test.Passed || test.ExitCode != -1 || test.Signal != "segmentation fault" {
		t.Fatalf("signaled test entry = %+v", test)
	}
	if !strings.Contains(test.Output, "crashing test marker") {
		t.Fatalf("signaled test output = %q", test.Output)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "VMAKE_E_TEST" {
		t.Fatalf("signaled test diagnostics = %+v", report.Diagnostics)
	}
}

func writePartialTestProject(t *testing.T, goodTestSource string) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "tests-project")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
		ctx.Target("checkgood").SetKind(api.TargetBinary).SetTest(true).AddFiles("tests/good.c")
		ctx.Target("checkbad").SetKind(api.TargetBinary).SetTest(true).AddFiles("tests/bad.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	writeExtensionFile(t, filepath.Join(project, "tests", "good.c"), goodTestSource)
	writeExtensionFile(t, filepath.Join(project, "tests", "bad.c"), "int main(void) {\n\tint value = ;\n\treturn value;\n}\n")
	return project
}

func TestTestCommandBuildFailureRecordsNoTests(t *testing.T) {
	project := writePartialTestProject(t, `#include <stdio.h>

int main(void) {
	printf("fail-fast test ran\n");
	return 0;
}
`)
	for _, args := range [][]string{{"test"}, {"test", "-k"}} {
		stdout, stderr, code := runBuildCommand(t, project, args...)
		if code != 1 {
			t.Fatalf("%v exit = %d, want 1 (stdout %q, stderr %q)", args, code, stdout, stderr)
		}
		if strings.Contains(stdout, "fail-fast test ran") || strings.Contains(stdout, "PASS tests-project:checkgood") {
			t.Fatalf("%v ran tests after build failure:\n%s", args, stdout)
		}
		report := loadTestReport(t, project)
		if report.Status != "failed" || report.Command != reportCommandTest || !strings.Contains(report.Error, "tests/bad.c:2:") {
			t.Fatalf("%v report = %+v", args, report)
		}
		if len(report.Tests) != 0 {
			t.Fatalf("%v tests = %+v, want none", args, report.Tests)
		}
		if report.Summary == nil || report.Summary.TestsFound != 0 || report.Summary.TestsPassed != 0 ||
			report.Summary.TestsFailed != 0 {
			t.Fatalf("%v summary = %+v", args, report.Summary)
		}
	}
}
