package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

func TestBuildReportActualInstallsAndInvalidation(t *testing.T) {
	dir := t.TempDir()
	state, root := filepath.Join(dir, "state"), filepath.Join(dir, "project")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "healthy", "toolchain.json"), "healthy")
	writeExtensionFile(t, filepath.Join(root, "build.go"), `package main
import ("os"; "fmt"; "github.com/spock2300/vmake/pkg/api")
func Main(p *api.Package) {
 p.SetRoot(true)
 p.OnInstall(func(c *api.InstallContext) { prefix, err := os.ReadFile("install-prefix"); if err != nil { panic(err) }; c.SetPrefix(string(prefix)) })
 p.OnConfig(func(c *api.ConfigContext) { c.Option("assets").SetType(api.OptionBool).SetDefault(true) })
 p.OnBuild(func(c *api.BuildContext) {
  c.Target("bindings").SetKind(api.TargetObject).SetPrebuilt("bindings-input")
  c.Target("runtime").SetKind(api.TargetStatic).SetPrebuilt("library-input").AddPublicIncludes("include", "@*.h")
  c.Target("firmware").SetKind(api.TargetBinary).SetPrebuilt("firmware-input").AddPostLinkOutputs("metadata.bin")
  c.Target("bundle").SetKind(api.TargetVoid).AddPostLinkOutputs("payload").SetBuildFunc(func(p *api.Package) error {
   if _, e := os.Stat("fail"); e == nil { return fmt.Errorf("fixture failure") }
   if _, e := os.Stat("change-config"); e == nil { if e := os.WriteFile(".vmake/config.json", []byte("{}"), 0644); e != nil { return e } }
   return os.WriteFile("payload", []byte("built bytes"), 0644)
  })
  if c.Bool("assets") { c.AddInstalls("resources", "share") }
 })
}
`)
	cfg := `{"version":"1","global":{"toolchain":"healthy"},"entries":{"project":{"options":{"assets":true}}}}`
	writeExtensionFile(t, filepath.Join(root, ".vmake", "config.json"), cfg)
	writeExtensionFile(t, filepath.Join(root, ".vmake", "plain.json"), strings.Replace(cfg, "true", "false", 1))
	writeExtensionFile(t, filepath.Join(root, "resources", "asset.elf"), "asset bytes")
	writeExtensionFile(t, filepath.Join(root, "bindings-input"), "object bytes")
	writeExtensionFile(t, filepath.Join(root, "library-input"), "library bytes")
	writeExtensionFile(t, filepath.Join(root, "firmware-input"), "firmware bytes")
	writeExtensionFile(t, filepath.Join(root, "metadata.bin"), "post-link bytes")
	writeExtensionFile(t, filepath.Join(root, "include", "api.h"), "header bytes")
	writeExtensionFile(t, filepath.Join(root, "include", "notes.txt"), "not a header")
	prefix := filepath.Join(dir, "custom-install")
	writeExtensionFile(t, filepath.Join(root, "install-prefix"), prefix)
	writeExtensionFile(t, filepath.Join(prefix, "share", "stale.elf"), "old bytes")
	run := func(ok bool, args ...string) {
		t.Helper()
		out, err := extensionCommand(t, state, root, args...)
		if (err == nil) != ok {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	read := func() buildReport {
		t.Helper()
		var report buildReport
		if err := jsonio.Load(reportPath(root), &report); err != nil {
			t.Fatal(err)
		}
		return report
	}
	run(true, "build", "-iV")
	first := read()
	if _, err := os.Stat(filepath.Join(prefix, "share", "stale.elf")); err != nil {
		t.Fatal("fixture must retain stale destination", err)
	}
	if first.Status != "succeeded" || len(first.Artifacts) != 3 {
		t.Fatalf("%+v", first)
	}
	objectFound := false
	for _, target := range first.Targets {
		if target.Target == "bindings" {
			objectFound = true
			if target.Kind != "object" || len(target.Outputs) != 0 {
				t.Fatalf("object target published intermediate outputs: %+v", target)
			}
		}
	}
	if !objectFound {
		t.Fatal("object target was not executed")
	}
	rawReport, err := os.ReadFile(reportPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var rawDocument struct {
		Targets []struct {
			Target  string `json:"target"`
			Kind    string `json:"kind"`
			Outputs []any  `json:"outputs"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(rawReport, &rawDocument); err != nil {
		t.Fatal(err)
	}
	for _, target := range rawDocument.Targets {
		if target.Target == "bindings" && (target.Kind != "object" || target.Outputs == nil || len(target.Outputs) != 0) {
			t.Fatalf("object target outputs must be an empty JSON array: %+v", target)
		}
	}
	for _, item := range first.Artifacts {
		if strings.Contains(item.BuildPath, "stale") || item.SHA256 == "" || item.InstallPath == "" || strings.HasSuffix(item.BuildPath, ".a") || filepath.Base(item.BuildPath) == "payload" {
			t.Fatalf("%+v", item)
		}
		hash, size, err := fileDigest(item.InstallPath)
		if err != nil || hash != item.SHA256 || size != item.Size {
			t.Fatalf("installed file identity: %+v, %v", item, err)
		}
		if strings.HasSuffix(item.BuildPath, "asset.elf") && (item.InstallPath != filepath.Join(prefix, "share", "asset.elf") || item.Target != "") {
			t.Fatalf("%+v", item)
		}
		if filepath.Base(item.BuildPath) == "metadata.bin" && (item.InstallPath != filepath.Join(prefix, "bin", "metadata.bin") || item.Target != "firmware") {
			t.Fatalf("%+v", item)
		}
	}
	run(true, "build", "-i")
	second := read()
	if second.BuildID == first.BuildID || len(second.Artifacts) != 3 {
		t.Fatalf("%+v", second)
	}
	run(true, "build")
	third := read()
	if third.Status != "succeeded" || third.Artifacts == nil || len(third.Artifacts) != 0 || len(third.Targets) == 0 {
		t.Fatalf("%+v", third)
	}
	run(true, "rebuild", "-i")
	if rebuilt := read(); rebuilt.Status != "succeeded" || len(rebuilt.Artifacts) != 3 {
		t.Fatalf("%+v", rebuilt)
	}
	run(true, "build", "-i", "--install-type", "sdk")
	sdk := read()
	if sdk.Status != "succeeded" || len(sdk.Artifacts) != 5 {
		t.Fatalf("%+v", sdk)
	}
	headerFound := false
	for _, item := range sdk.Artifacts {
		if filepath.Base(item.BuildPath) == "notes.txt" {
			t.Fatalf("filtered public include in artifacts: %+v", item)
		}
		if filepath.Base(item.BuildPath) == "api.h" {
			headerFound = true
			if item.Target != "runtime" || item.InstallPath != filepath.Join(prefix, "include", "api.h") {
				t.Fatalf("public include artifact: %+v", item)
			}
		}
	}
	if !headerFound {
		t.Fatal("public include was not recorded")
	}
	run(true, "config", "use", "plain.json")
	run(true, "build", "-i")
	plain := read()
	if plain.ConfigFile != "plain.json" || len(plain.Artifacts) != 2 {
		t.Fatalf("%+v", plain)
	}
	writeExtensionFile(t, filepath.Join(root, "fail"), "")
	run(false, "build")
	failed := read()
	if failed.Status != "failed" || len(failed.Artifacts) != 0 || failed.BuildID == plain.BuildID {
		t.Fatalf("%+v", failed)
	}
	if err := os.Remove(filepath.Join(root, "fail")); err != nil {
		t.Fatal(err)
	}
	run(true, "config", "use", "config.json")
	writeExtensionFile(t, filepath.Join(root, "change-config"), "")
	run(false, "build")
	if changed := read(); changed.Status != "failed" || !strings.Contains(changed.Error, "configuration changed") {
		t.Fatalf("%+v", changed)
	}
	run(true, "distclean")
	if _, err := os.Stat(reportPath(root)); !os.IsNotExist(err) {
		t.Fatalf("report survived clean: %v", err)
	}
}

func TestBuildReportNeverSucceedsBeforeCompletion(t *testing.T) {
	for _, failure := range []error{context.Canceled, errors.New("failed"), nil} {
		r := &buildReport{Root: t.TempDir(), Status: "running"}
		result := failure
		r.finish(&result)
		if result == nil || r.Status == "succeeded" {
			t.Fatalf("%+v / %v", r, result)
		}
		if errors.Is(failure, context.Canceled) && (r.Status != "cancelled" || r.Error != "build cancelled") {
			t.Fatalf("cancelled report = %+v", r)
		}
		data, err := os.ReadFile(reportPath(r.Root))
		if err != nil {
			t.Fatal(err)
		}
		var saved buildReport
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Status != r.Status || len(saved.Artifacts) != 0 || saved.Diagnostics == nil {
			t.Fatalf("%+v", saved)
		}
	}
}

func TestBuildReportPanicCannotPublishSuccess(t *testing.T) {
	r := &buildReport{Root: t.TempDir(), completed: true}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		var err error
		defer r.finish(&err)
		panic("unexpected interruption")
	}()
	var saved buildReport
	if err := jsonio.Load(reportPath(r.Root), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Status != "failed" || len(saved.Artifacts) != 0 {
		t.Fatalf("%+v", saved)
	}
}

func TestBuildReportBootstrapChild(t *testing.T) {
	dir := os.Getenv("VMAKE_TEST_BUILD_DIR")
	if dir == "" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing command arguments")
	}
	vmakeDir = dir
	os.Args = append([]string{"vmake"}, os.Args[separator+1:]...)
	main()
	os.Exit(0)
}

func runBuildCommand(t *testing.T, projectDir string, args ...string) (string, string, int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commandArgs := append([]string{"-test.run=^TestBuildReportBootstrapChild$", "--"}, args...)
	cmd := exec.Command(exe, commandArgs...)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(),
		"VMAKE_TEST_BUILD_DIR="+t.TempDir(),
		"VMAKE_CACHE="+t.TempDir(),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run build: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func writeBuildReportProject(t *testing.T, source string) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "scratch")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), source)
	return project
}

func loadBuildReport(t *testing.T, project string) buildReport {
	t.Helper()
	var report buildReport
	if err := jsonio.Load(reportPath(project), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestBuildReportV2Success(t *testing.T) {
	project := writeBuildReportProject(t, "int main(void) { return 0; }\n")
	stdout, stderr, code := runBuildCommand(t, project, "build")
	if code != 0 || stderr != "" {
		t.Fatalf("build exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Build succeeded!") {
		t.Fatalf("success stdout changed:\n%s", stdout)
	}
	report := loadBuildReport(t, project)
	if report.SchemaVersion != 2 || report.Status != "succeeded" || report.Error != "" {
		t.Fatalf("report = %+v", report)
	}
	if report.DurationMs <= 0 {
		t.Fatalf("durationMs = %d", report.DurationMs)
	}
	if report.Diagnostics == nil || len(report.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want []", report.Diagnostics)
	}
	if report.BuildID == "" || report.Root != project || report.Mode == "" || report.Toolchain == "" || len(report.Targets) == 0 {
		t.Fatalf("v1 fields lost: %+v", report)
	}
	if report.Summary == nil {
		t.Fatal("successful report has no summary")
	}
	summary := report.Summary
	if summary.TargetsTotal != 1 || summary.TargetsBuilt != 1 || summary.TargetsUpToDate != 0 || summary.TargetsFailed != 0 ||
		summary.FilesCompiled != 1 || summary.FilesLinked != 1 || summary.ArtifactsInstalled != 0 ||
		!slices.Equal(summary.ChangedFiles, []string{"src/main.c"}) {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.DurationMs <= 0 || summary.DurationMs != report.DurationMs {
		t.Fatalf("summary duration = %d, report duration = %d", summary.DurationMs, report.DurationMs)
	}
	if len(report.Targets) != 1 || report.Targets[0].Status != build.TargetStatusBuilt || report.Targets[0].DurationMs < 0 {
		t.Fatalf("target result = %+v", report.Targets)
	}
	if !strings.Contains(stdout, "Build succeeded: 1 targets (1 built, 0 up to date), 1 compiled, 1 linked, 0 installed in ") ||
		!strings.Contains(stdout, "(buildId="+report.BuildID+")") {
		t.Fatalf("success summary line missing or wrong:\n%s", stdout)
	}
	raw, err := os.ReadFile(reportPath(project))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"diagnostics": []`) {
		t.Fatalf("success report diagnostics are not an empty JSON array:\n%s", raw)
	}
}

func TestBuildReportV2CompileFailure(t *testing.T) {
	project := writeBuildReportProject(t, "int main(void) {\n\tint value = ;\n\treturn value;\n}\n")
	stdout, stderr, code := runBuildCommand(t, project, "build")
	if code != 1 {
		t.Fatalf("build exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	report := loadBuildReport(t, project)
	if report.SchemaVersion != 2 || report.Status != "failed" {
		t.Fatalf("report = %+v", report)
	}
	if report.Error == "" || len(report.Error) >= 300 || strings.Contains(report.Error, "\n") || strings.Contains(report.Error, " -I") {
		t.Fatalf("concise error = %q", report.Error)
	}
	if !strings.Contains(report.Error, "src/main.c:2:") {
		t.Fatalf("error does not point at the source: %q", report.Error)
	}
	if len(report.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", report.Diagnostics)
	}
	diagnostic := report.Diagnostics[0]
	if diagnostic.Severity != "error" || diagnostic.Code != "VMAKE_E_COMPILE" ||
		diagnostic.File != "src/main.c" || diagnostic.Line != 2 || diagnostic.Column == 0 {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	if diagnostic.Package != "scratch" || diagnostic.Target != "scratch:app" || diagnostic.Message == "" {
		t.Fatalf("diagnostic context = %+v", diagnostic)
	}
	if strings.Count(stderr, "error: expected expression") != 1 {
		t.Fatalf("compiler diagnostic on stderr %d times:\n%s", strings.Count(stderr, "error: expected expression"), stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	final := lines[len(lines)-1]
	if !strings.HasPrefix(final, "Error: ") {
		t.Fatalf("final stderr line = %q", final)
	}
	summarized := strings.TrimPrefix(final, "Error: ")
	if summarized != report.Error || len(summarized) >= 300 {
		t.Fatalf("final line %q != report error %q", summarized, report.Error)
	}
}

func TestBuildReportV2LinkFallback(t *testing.T) {
	project := writeBuildReportProject(t, "extern int missing_function(void);\nint main(void) { return missing_function(); }\n")
	_, stderr, code := runBuildCommand(t, project, "build")
	if code != 1 {
		t.Fatalf("build exit = %d, want 1 (stderr %q)", code, stderr)
	}
	report := loadBuildReport(t, project)
	if report.Status != "failed" || len(report.Diagnostics) != 1 {
		t.Fatalf("report = %+v", report)
	}
	diagnostic := report.Diagnostics[0]
	if diagnostic.Code != "VMAKE_E_LINK" || diagnostic.Severity != "error" || diagnostic.File != "" {
		t.Fatalf("fallback diagnostic = %+v", diagnostic)
	}
	if diagnostic.Package != "scratch" || diagnostic.Target != "scratch:app" || diagnostic.Message == "" {
		t.Fatalf("fallback context = %+v", diagnostic)
	}
	if report.Error == "" || len(report.Error) >= 300 || strings.Contains(report.Error, "\n") {
		t.Fatalf("fallback summary = %q", report.Error)
	}
}

func TestBuildReportV2ScriptError(t *testing.T) {
	project := filepath.Join(t.TempDir(), "scratch")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Bool("definitely_not_an_option")
	})
}
`)
	_, stderr, code := runBuildCommand(t, project, "build")
	if code != 1 {
		t.Fatalf("build exit = %d, want 1 (stderr %q)", code, stderr)
	}
	report := loadBuildReport(t, project)
	if report.Status != "failed" || len(report.Diagnostics) != 1 {
		t.Fatalf("report = %+v", report)
	}
	diagnostic := report.Diagnostics[0]
	if diagnostic.Code != "VMAKE_E_SCRIPT" || diagnostic.Severity != "error" || diagnostic.Package != "scratch" {
		t.Fatalf("script diagnostic = %+v", diagnostic)
	}
	if !strings.Contains(diagnostic.Message, "unknown option") || report.Error != diagnostic.Message {
		t.Fatalf("script summary = %q / %q", report.Error, diagnostic.Message)
	}
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	if !strings.HasPrefix(lines[len(lines)-1], "Error: ") {
		t.Fatalf("final stderr line = %q", lines[len(lines)-1])
	}
}

func TestBuildFailureDetails(t *testing.T) {
	commandErr := &iexec.CommandError{
		Command: "gcc -c src/main.c",
		Output:  "src/main.c:7:3: warning: unused variable\nsrc/main.c:9:11: error: expected ';' before '}' token\n",
		Err:     errors.New("exit status 1"),
	}
	targetErr := &build.TargetError{Target: "firmware:firmware", Stage: "compile", Err: commandErr}
	diagnostics, summary := buildFailureDetails(targetErr)
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if diagnostics[0].Severity != "warning" || diagnostics[0].Line != 7 || diagnostics[0].Column != 3 {
		t.Fatalf("warning diagnostic = %+v", diagnostics[0])
	}
	errorDiagnostic := diagnostics[1]
	if errorDiagnostic.Code != "VMAKE_E_COMPILE" || errorDiagnostic.File != "src/main.c" || errorDiagnostic.Line != 9 ||
		errorDiagnostic.Column != 11 || errorDiagnostic.Package != "firmware" || errorDiagnostic.Target != "firmware:firmware" {
		t.Fatalf("error diagnostic = %+v", errorDiagnostic)
	}
	if summary != "src/main.c:9:11: expected ';' before '}' token" {
		t.Fatalf("summary = %q", summary)
	}

	fallback := &build.TargetError{Target: "app", Stage: "link", Err: &iexec.CommandError{Command: "ld", Output: "collect2: fatal: cannot find ld\n", Err: errors.New("exit status 1")}}
	diagnostics, summary = buildFailureDetails(fallback)
	if len(diagnostics) != 1 || diagnostics[0].Code != "VMAKE_E_LINK" || diagnostics[0].Severity != "error" ||
		diagnostics[0].Message != "collect2: fatal: cannot find ld" || diagnostics[0].Package != "app" || diagnostics[0].Target != "app" {
		t.Fatalf("fallback = %+v", diagnostics)
	}
	if summary != "collect2: fatal: cannot find ld" {
		t.Fatalf("fallback summary = %q", summary)
	}

	scriptErr := &api.BuildScriptError{Package: "firmware", Op: "Bool", Err: errors.New(`unknown option "nope"`)}
	diagnostics, summary = buildFailureDetails(scriptErr)
	if len(diagnostics) != 1 || diagnostics[0].Code != "VMAKE_E_SCRIPT" || diagnostics[0].Package != "firmware" {
		t.Fatalf("script = %+v", diagnostics)
	}
	if summary != `package firmware: Bool: unknown option "nope"` {
		t.Fatalf("script summary = %q", summary)
	}

	diagnostics, summary = buildFailureDetails(context.Canceled)
	if len(diagnostics) != 0 || summary != "build cancelled" {
		t.Fatalf("cancellation = %+v / %q", diagnostics, summary)
	}

	long := strings.Repeat("x", 500)
	diagnostics, summary = buildFailureDetails(errors.New(long + "\nsecond line"))
	if len(diagnostics) != 0 || len(summary) >= 300 || strings.HasSuffix(summary, "second line") {
		t.Fatalf("long summary = %q", summary)
	}
}

func TestBuildReportFinishV2(t *testing.T) {
	root := t.TempDir()
	report := &buildReport{
		SchemaVersion: 2,
		Root:          root,
		Status:        "running",
		Started:       time.Now().Add(-25 * time.Millisecond).UTC().Format(time.RFC3339Nano),
		Artifacts:     []buildArtifact{{Package: "stale"}},
	}
	var result error = &build.TargetError{
		Target: "firmware:firmware",
		Stage:  "compile",
		Err:    &iexec.CommandError{Command: "gcc", Output: "src/main.c:4:2: error: boom\n", Err: errors.New("exit status 1")},
	}
	report.finish(&result)
	if report.Status != "failed" || report.Error != "src/main.c:4:2: boom" || len(report.Diagnostics) != 1 || report.DurationMs <= 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Artifacts) != 0 {
		t.Fatalf("failed report kept artifacts: %+v", report.Artifacts)
	}
	var targetErr *build.TargetError
	if !errors.As(result, &targetErr) {
		t.Fatalf("finish dropped the error tree: %v", result)
	}
	var saved buildReport
	if err := jsonio.Load(reportPath(root), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.SchemaVersion != 2 || saved.Status != "failed" || saved.Error != report.Error || len(saved.Diagnostics) != 1 {
		t.Fatalf("saved report = %+v", saved)
	}
	if saved.DurationMs <= 0 {
		t.Fatalf("saved duration = %d", saved.DurationMs)
	}
}

func TestBuildSummaryErrorPreservesCause(t *testing.T) {
	err := &buildSummaryError{summary: "build cancelled", err: &buildInterrupted{code: 130}}
	if err.Error() != "build cancelled" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("context.Canceled is not discoverable through the summary wrapper")
	}
	var interrupted *buildInterrupted
	if !errors.As(err, &interrupted) || interrupted.code != 130 {
		t.Fatalf("buildInterrupted is not discoverable through the summary wrapper: %v", err)
	}
}

func writeBuildSummaryProject(t *testing.T, source string) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "summary")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import (
	"fmt"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		fmt.Println("SCRIPT NOISE")
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), source)
	return project
}

func TestBuildQuietSummaryAndReportCounts(t *testing.T) {
	project := writeBuildSummaryProject(t, "int main(void) { return 0; }\n")
	quietBuild := func(args ...string) string {
		t.Helper()
		stdout, stderr, code := runBuildCommand(t, project, append([]string{"-q", "build"}, args...)...)
		if code != 0 {
			t.Fatalf("quiet build exit = %d, stderr = %q", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("quiet success stderr = %q", stderr)
		}
		lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "Build succeeded: ") {
			t.Fatalf("quiet stdout = %q, want one summary line", stdout)
		}
		if strings.Contains(stdout, "SCRIPT NOISE") {
			t.Fatalf("buildscript stdout leaked into quiet output: %q", stdout)
		}
		return lines[0]
	}

	cold := quietBuild("-i")
	for _, want := range []string{"1 targets (1 built, 0 up to date)", "1 compiled, 1 linked, 1 installed", "(buildId="} {
		if !strings.Contains(cold, want) {
			t.Fatalf("cold summary %q missing %q", cold, want)
		}
	}
	report := loadBuildReport(t, project)
	if report.Summary == nil || report.Summary.TargetsBuilt != 1 || report.Summary.TargetsUpToDate != 0 ||
		report.Summary.FilesCompiled != 1 || report.Summary.FilesLinked != 1 || report.Summary.ArtifactsInstalled != 1 {
		t.Fatalf("cold summary report = %+v", report.Summary)
	}
	if !slices.Equal(report.Summary.ChangedFiles, []string{"src/main.c"}) {
		t.Fatalf("cold changed files = %v", report.Summary.ChangedFiles)
	}
	if len(report.Targets) != 1 || report.Targets[0].Status != build.TargetStatusBuilt || report.Targets[0].DurationMs < 0 {
		t.Fatalf("cold target results = %+v", report.Targets)
	}

	noop := quietBuild("-i")
	if !strings.Contains(noop, "(0 built, 1 up to date)") || !strings.Contains(noop, "0 compiled, 0 linked") {
		t.Fatalf("no-op summary = %q", noop)
	}
	report = loadBuildReport(t, project)
	if report.Summary == nil || report.Summary.TargetsBuilt != 0 || report.Summary.TargetsUpToDate != 1 ||
		report.Summary.FilesCompiled != 0 || report.Summary.FilesLinked != 0 || len(report.Summary.ChangedFiles) != 0 {
		t.Fatalf("no-op summary report = %+v", report.Summary)
	}
	if report.Summary.ActivitiesUpToDate < 2 {
		t.Fatalf("no-op actionsUpToDate = %d", report.Summary.ActivitiesUpToDate)
	}
	if report.Summary.ChangedFiles == nil {
		t.Fatal("no-op changedFiles is null, want []")
	}
	if report.Targets[0].Status != build.TargetStatusUpToDate {
		t.Fatalf("no-op target status = %q", report.Targets[0].Status)
	}

	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 1; }\n")
	incremental := quietBuild("-i")
	if !strings.Contains(incremental, "(1 built, 0 up to date)") || !strings.Contains(incremental, "1 compiled, 1 linked") {
		t.Fatalf("incremental summary = %q", incremental)
	}
	report = loadBuildReport(t, project)
	if report.Summary == nil || report.Summary.TargetsBuilt != 1 || report.Summary.FilesCompiled != 1 ||
		report.Summary.FilesLinked != 1 || !slices.Equal(report.Summary.ChangedFiles, []string{"src/main.c"}) {
		t.Fatalf("incremental summary report = %+v", report.Summary)
	}
	if report.Targets[0].Status != build.TargetStatusBuilt {
		t.Fatalf("incremental target status = %q", report.Targets[0].Status)
	}

	stdout, stderr, code := runBuildCommand(t, project, "build")
	if code != 0 || stderr != "" {
		t.Fatalf("default build exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "SCRIPT NOISE") {
		t.Fatalf("default output suppressed buildscript stdout: %q", stdout)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if !strings.HasPrefix(lines[len(lines)-1], "Build succeeded: ") {
		t.Fatalf("default stdout does not end with the summary: %q", lines[len(lines)-1])
	}

	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) {\n\tint value = ;\n\treturn value;\n}\n")
	stdout, stderr, code = runBuildCommand(t, project, "-q", "build")
	if code != 1 {
		t.Fatalf("quiet failure exit = %d, stderr = %q", code, stderr)
	}
	lines = strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "Build failed: src/main.c:2:") {
		t.Fatalf("quiet failure stdout = %q", stdout)
	}
	if strings.Contains(stdout, "SCRIPT NOISE") {
		t.Fatalf("quiet failure leaked buildscript stdout: %q", stdout)
	}
	if strings.Count(stderr, "error: expected expression") != 1 {
		t.Fatalf("compiler diagnostic count = %d:\n%s", strings.Count(stderr, "error: expected expression"), stderr)
	}
	stderrLines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	if !strings.HasPrefix(stderrLines[len(stderrLines)-1], "Error: src/main.c:2:") {
		t.Fatalf("final stderr line = %q", stderrLines[len(stderrLines)-1])
	}
	report = loadBuildReport(t, project)
	if report.Status != "failed" || report.Summary != nil || len(report.Diagnostics) != 1 {
		t.Fatalf("failure report = %+v", report)
	}
}

func TestBuildReportVoidTargetStatus(t *testing.T) {
	project := filepath.Join(t.TempDir(), "tasks")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("task").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error { return nil })
	})
}
`)
	stdout, stderr, code := runBuildCommand(t, project, "-q", "build")
	if code != 0 || stderr != "" {
		t.Fatalf("build exit = %d, stderr = %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "(1 built, 0 up to date)") || !strings.Contains(lines[0], "0 compiled, 0 linked") {
		t.Fatalf("void summary = %q", stdout)
	}
	report := loadBuildReport(t, project)
	if report.Summary == nil || report.Summary.TargetsBuilt != 1 || report.Summary.TargetsUpToDate != 0 ||
		report.Summary.FilesCompiled != 0 || report.Summary.FilesLinked != 0 || len(report.Summary.ChangedFiles) != 0 {
		t.Fatalf("void summary report = %+v", report.Summary)
	}
	if len(report.Targets) != 1 || report.Targets[0].Status != build.TargetStatusBuilt || report.Targets[0].DurationMs < 0 {
		t.Fatalf("void target result = %+v", report.Targets)
	}
}
