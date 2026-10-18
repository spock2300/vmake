package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		r.finish(&result, nil)
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
		defer r.finish(&err, nil)
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
	if report.Command != reportCommandBuild {
		t.Fatalf("command = %q, want %q", report.Command, reportCommandBuild)
	}
	if len(report.Global) == 0 || report.Global["mode"] != report.Mode || report.Global["toolchain"] != report.Toolchain {
		t.Fatalf("global identity = %+v (mode %q, toolchain %q)", report.Global, report.Mode, report.Toolchain)
	}
	if report.Tests == nil || len(report.Tests) != 0 {
		t.Fatalf("tests = %#v, want []", report.Tests)
	}
	if report.Summary == nil {
		t.Fatal("successful report has no summary")
	}
	summary := report.Summary
	if summary.TargetsTotal != 1 || summary.TargetsBuilt != 1 || summary.TargetsUpToDate != 0 || summary.TargetsFailed != 0 ||
		summary.TestsFound != 0 || summary.TestsPassed != 0 || summary.TestsFailed != 0 ||
		summary.FilesCompiled != 1 || summary.FilesLinked != 1 || summary.ArtifactsInstalled != 0 ||
		!slices.Equal(summary.ChangedFiles, []string{"scratch:src/main.c"}) {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Callbacks != 0 || summary.NoOp {
		t.Fatalf("compiled build reported callbacks/no-op: %+v", summary)
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
	writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","global":{"mode":"release"}}`)
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
	if report.Toolchain == "" || report.Mode == "" || len(report.Global) == 0 ||
		report.Global[api.ModeOptionName] != report.Mode || report.Global[api.ToolchainOptionName] != report.Toolchain {
		t.Fatalf("failure identity = %+v (toolchain %q, mode %q)", report.Global, report.Toolchain, report.Mode)
	}
	if report.Mode != api.ModeRelease || report.ConfigFile != "config.json" {
		t.Fatalf("failure identity ignored the selected config: mode %q, configFile %q", report.Mode, report.ConfigFile)
	}
	wantDatabase := filepath.Join(project, "build", "compile_commands.json")
	if report.CompileDatabase != wantDatabase {
		t.Fatalf("compileDatabase = %q, want %q", report.CompileDatabase, wantDatabase)
	}
	if _, err := os.Stat(report.CompileDatabase); err != nil {
		t.Fatalf("compile database missing: %v", err)
	}
	if len(report.Targets) != 0 || len(report.Artifacts) != 0 || report.Summary == nil {
		t.Fatalf("failed build report lost its partial summary: %+v", report)
	}
	if summary := report.Summary; summary.TargetsTotal != 0 || summary.TargetsBuilt != 0 ||
		summary.TargetsUpToDate != 0 || summary.TargetsFailed != 1 || summary.FilesLinked != 0 ||
		summary.ArtifactsInstalled != 0 {
		t.Fatalf("partial failure summary = %+v", summary)
	}
	if report.CompileDatabase == "" || len(report.Packages) == 0 {
		t.Fatalf("failed build report lost partial identity: %+v", report)
	}
}

func writeTwoTargetBuildReportProject(t *testing.T, firstSource, secondSource string) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "partial-failure")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("first").SetKind(api.TargetBinary).AddFiles("src/first.c")
		ctx.Target("second").SetKind(api.TargetBinary).AddFiles("src/second.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "first.c"), firstSource)
	writeExtensionFile(t, filepath.Join(project, "src", "second.c"), secondSource)
	return project
}

func TestBuildReportPartialFailureKeepsCompletedTargets(t *testing.T) {
	project := writeTwoTargetBuildReportProject(t,
		"int main(void) { return 0; }\n",
		"int main(void) {\n\tint value = ;\n\treturn value;\n}\n")
	stdout, stderr, code := runBuildCommand(t, project, "build")
	if code != 1 {
		t.Fatalf("build exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Build failed: src/second.c:2:") {
		t.Fatalf("failure summary missing:\n%s", stdout)
	}
	report := loadBuildReport(t, project)
	if report.Status != "failed" || len(report.Diagnostics) != 1 {
		t.Fatalf("report = %+v", report)
	}
	diagnostic := report.Diagnostics[0]
	if diagnostic.Code != "VMAKE_E_COMPILE" || diagnostic.File != "src/second.c" ||
		diagnostic.Line != 2 || diagnostic.Target != "partial-failure:second" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	if len(report.Targets) != 1 || report.Targets[0].Target != "first" || report.Targets[0].Status != build.TargetStatusBuilt {
		t.Fatalf("completed targets = %+v", report.Targets)
	}
	if len(report.Artifacts) != 0 {
		t.Fatalf("failed build kept artifacts: %+v", report.Artifacts)
	}
	summary := report.Summary
	if summary == nil || summary.TargetsTotal != 1 || summary.TargetsBuilt != 1 ||
		summary.TargetsUpToDate != 0 || summary.TargetsFailed != 1 ||
		summary.FilesCompiled < 1 || summary.FilesLinked != 1 || summary.ArtifactsInstalled != 0 {
		t.Fatalf("partial summary = %+v", summary)
	}
	if !slices.Contains(summary.ChangedFiles, "partial-failure:src/first.c") {
		t.Fatalf("partial changed files = %v", summary.ChangedFiles)
	}
	if report.Toolchain == "" || report.Mode == "" || len(report.Global) == 0 ||
		report.Global[api.ModeOptionName] != report.Mode ||
		report.Global[api.ToolchainOptionName] != report.Toolchain ||
		len(report.Packages) == 0 || report.Packages["partial-failure"] == nil {
		t.Fatalf("partial identity = %+v (toolchain %q, mode %q)", report.Global, report.Toolchain, report.Mode)
	}
	wantDatabase := filepath.Join(project, "build", "compile_commands.json")
	if report.CompileDatabase != wantDatabase {
		t.Fatalf("compileDatabase = %q, want %q", report.CompileDatabase, wantDatabase)
	}
	if _, err := os.Stat(report.CompileDatabase); err != nil {
		t.Fatalf("compile database missing: %v", err)
	}
	if !strings.Contains(stderr, "error: expected expression") {
		t.Fatalf("raw diagnostic missing from stderr:\n%s", stderr)
	}
}

func TestBuildReportKeepGoingKeepsBothDiagnostics(t *testing.T) {
	project := writeTwoTargetBuildReportProject(t,
		"int main(void) {\n\tint first = ;\n\treturn first;\n}\n",
		"int main(void) {\n\tint second = ;\n\treturn second;\n}\n")
	stdout, stderr, code := runBuildCommand(t, project, "build", "-k")
	if code != 1 {
		t.Fatalf("build -k exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	report := loadBuildReport(t, project)
	if report.Status != "failed" || len(report.Diagnostics) != 2 {
		t.Fatalf("keep-going report = %+v", report)
	}
	failedTargets := map[string]bool{}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code != "VMAKE_E_COMPILE" || diagnostic.File == "" || diagnostic.Line != 2 {
			t.Fatalf("keep-going diagnostic = %+v", diagnostic)
		}
		failedTargets[diagnostic.Target] = true
	}
	if !failedTargets["partial-failure:first"] || !failedTargets["partial-failure:second"] {
		t.Fatalf("keep-going targets = %v", failedTargets)
	}
	if len(report.Targets) != 0 || len(report.Artifacts) != 0 {
		t.Fatalf("keep-going report invented completed work: %+v", report)
	}
	if report.Summary == nil || report.Summary.TargetsFailed != 2 || report.Summary.TargetsBuilt != 0 {
		t.Fatalf("keep-going summary = %+v", report.Summary)
	}
}

func TestBuildReportKeepGoingKeepsLaterSuccess(t *testing.T) {
	project := writeTwoTargetBuildReportProject(t,
		"int main(void) {\n\tint first = ;\n\treturn first;\n}\n",
		"int main(void) { return 0; }\n")
	_, _, code := runBuildCommand(t, project, "build", "-k")
	if code != 1 {
		t.Fatalf("build -k exit = %d, want 1", code)
	}
	report := loadBuildReport(t, project)
	if report.Status != "failed" || len(report.Diagnostics) != 1 || report.Diagnostics[0].Target != "partial-failure:first" {
		t.Fatalf("keep-going report = %+v", report)
	}
	if len(report.Targets) != 1 || report.Targets[0].Target != "second" || report.Targets[0].Status != build.TargetStatusBuilt {
		t.Fatalf("keep-going completed targets = %+v", report.Targets)
	}
	if report.Summary == nil || report.Summary.TargetsBuilt != 1 || report.Summary.TargetsFailed != 1 ||
		report.Summary.FilesLinked != 1 {
		t.Fatalf("keep-going summary = %+v", report.Summary)
	}
}

func TestBuildReportV2LinkFailure(t *testing.T) {
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
	if diagnostic.Code != "VMAKE_E_LINK" || diagnostic.Severity != "error" ||
		filepath.Base(diagnostic.File) != "main.c" || diagnostic.Line != 2 {
		t.Fatalf("link diagnostic = %+v", diagnostic)
	}
	if !strings.Contains(diagnostic.Message, "undefined reference to") || !strings.Contains(diagnostic.Message, "missing_function") {
		t.Fatalf("link diagnostic message = %q", diagnostic.Message)
	}
	if diagnostic.Package != "scratch" || diagnostic.Target != "scratch:app" || diagnostic.Message == "" {
		t.Fatalf("link context = %+v", diagnostic)
	}
	if report.Error == "" || len(report.Error) >= 300 || strings.Contains(report.Error, "\n") {
		t.Fatalf("link summary = %q", report.Error)
	}
	if !strings.Contains(report.Error, "undefined reference to") || !strings.Contains(report.Error, "missing_function") {
		t.Fatalf("link summary does not name the unresolved symbol: %q", report.Error)
	}
	if strings.Contains(report.Error, "/object/") || strings.Contains(report.Error, filepath.Join(project, "build")) {
		t.Fatalf("link summary leaked build-key paths: %q", report.Error)
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
	if report.Toolchain == "" || report.Mode == "" || len(report.Global) == 0 {
		t.Fatalf("script failure identity = %+v (toolchain %q, mode %q)", report.Global, report.Toolchain, report.Mode)
	}
	if report.CompileDatabase != "" {
		t.Fatalf("script failure before target execution recorded compileDatabase %q", report.CompileDatabase)
	}
	if len(report.Targets) != 0 || len(report.Artifacts) != 0 || report.Summary != nil {
		t.Fatalf("script failure invented results: %+v", report)
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

	longPath := "/tmp/" + strings.Repeat("object-hash", 40) + "/main.o"
	symbolFallback := &build.TargetError{Target: "firmware:app", Stage: "link", Err: &iexec.CommandError{
		Command: "ld",
		Output:  longPath + ": in function `call':\n/usr/bin/ld: main.o: undefined reference to `wanted_symbol'\ncollect2: error: ld returned 1 exit status\n",
		Err:     errors.New("exit status 1"),
	}}
	diagnostics, summary = buildFailureDetails(symbolFallback)
	if len(diagnostics) != 1 || diagnostics[0].Code != "VMAKE_E_LINK" || diagnostics[0].File != "" ||
		!strings.Contains(diagnostics[0].Message, "undefined reference to `wanted_symbol'") {
		t.Fatalf("symbol fallback = %+v", diagnostics)
	}
	if summary != "/usr/bin/ld: main.o: undefined reference to `wanted_symbol'" {
		t.Fatalf("symbol fallback summary = %q", summary)
	}

	errorFallback := &build.TargetError{Target: "firmware:app", Stage: "link", Err: &iexec.CommandError{
		Command: "ld",
		Output:  longPath + ": cannot open\ncollect2: error: ld returned 1 exit status\n",
		Err:     errors.New("exit status 1"),
	}}
	diagnostics, summary = buildFailureDetails(errorFallback)
	if len(diagnostics) != 1 || summary != "collect2: error: ld returned 1 exit status" {
		t.Fatalf("error fallback = %+v / %q", diagnostics, summary)
	}
}

func TestGNUDiagnosticsParsesLinkerSymbols(t *testing.T) {
	output := strings.Join([]string{
		"/usr/bin/ld: /tmp/build/key/object/hash/main.o: in function `call':",
		"/tmp/project/src/main.c:15: undefined reference to `missing_symbol'",
		"src/generated.c:9:(.text.call+0x5): undefined reference to `other_symbol'",
		"src/duplicate.c:7: multiple definition of `duplicate_symbol'",
		"collect2: error: ld returned 1 exit status",
	}, "\n")
	diagnostics := parseGNUDiagnostics(output)
	if len(diagnostics) != 3 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	for i, want := range []struct {
		file    string
		line    int
		message string
	}{
		{"/tmp/project/src/main.c", 15, "undefined reference to `missing_symbol'"},
		{"src/generated.c", 9, "undefined reference to `other_symbol'"},
		{"src/duplicate.c", 7, "multiple definition of `duplicate_symbol'"},
	} {
		got := diagnostics[i]
		if got.Severity != "error" || got.Code != "" || got.Column != 0 || got.File != want.file || got.Line != want.line || got.Message != want.message {
			t.Fatalf("diagnostic %d = %+v, want file %q line %d message %q", i, got, want.file, want.line, want.message)
		}
	}
}

func TestBuildFailureDetailsLinkerDiagnostic(t *testing.T) {
	output := strings.Join([]string{
		"/usr/bin/ld: /tmp/build/key/object/hash/main.o: in function `call':",
		"/tmp/project/src/main.c:15:(.text.call+0x5): undefined reference to `missing_symbol'",
		"collect2: error: ld returned 1 exit status",
	}, "\n")
	targetErr := &build.TargetError{Target: "firmware:app", Stage: "link", Err: &iexec.CommandError{Command: "gcc", Output: output, Err: errors.New("exit status 1")}}
	diagnostics, summary := buildFailureDetails(targetErr)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Code != "VMAKE_E_LINK" || diagnostic.Severity != "error" ||
		diagnostic.File != "/tmp/project/src/main.c" || diagnostic.Line != 15 ||
		diagnostic.Package != "firmware" || diagnostic.Target != "firmware:app" ||
		diagnostic.Message != "undefined reference to `missing_symbol'" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	if summary != "/tmp/project/src/main.c:15: undefined reference to `missing_symbol'" {
		t.Fatalf("summary = %q", summary)
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
	report.finish(&result, nil)
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
	if !slices.Equal(report.Summary.ChangedFiles, []string{"summary:src/main.c"}) {
		t.Fatalf("cold changed files = %v", report.Summary.ChangedFiles)
	}
	if report.Summary.Callbacks != 0 || report.Summary.NoOp {
		t.Fatalf("cold summary callbacks/no-op = %+v", report.Summary)
	}
	if len(report.Targets) != 1 || report.Targets[0].Status != build.TargetStatusBuilt || report.Targets[0].DurationMs < 0 {
		t.Fatalf("cold target results = %+v", report.Targets)
	}

	noop := quietBuild("-i")
	if !strings.HasPrefix(noop, "Build succeeded: no compilation (1 targets, 1 up to date, 0 callbacks,") {
		t.Fatalf("no-op summary = %q", noop)
	}
	report = loadBuildReport(t, project)
	if report.Summary == nil || report.Summary.TargetsBuilt != 0 || report.Summary.TargetsUpToDate != 1 ||
		report.Summary.FilesCompiled != 0 || report.Summary.FilesLinked != 0 || len(report.Summary.ChangedFiles) != 0 {
		t.Fatalf("no-op summary report = %+v", report.Summary)
	}
	if !report.Summary.NoOp || report.Summary.Callbacks != 0 {
		t.Fatalf("no-op summary callbacks/no-op = %+v", report.Summary)
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
		report.Summary.FilesLinked != 1 || !slices.Equal(report.Summary.ChangedFiles, []string{"summary:src/main.c"}) {
		t.Fatalf("incremental summary report = %+v", report.Summary)
	}
	if report.Summary.NoOp || report.Summary.Callbacks != 0 {
		t.Fatalf("incremental summary callbacks/no-op = %+v", report.Summary)
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
	if report.Status != "failed" || report.Summary == nil || report.Summary.TargetsFailed != 1 || len(report.Diagnostics) != 1 {
		t.Fatalf("failure report = %+v (summary %+v)", report, report.Summary)
	}
}

func TestBuildReportVoidTargetStatus(t *testing.T) {
	project := filepath.Join(t.TempDir(), "tasks")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
		ctx.Target("task").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error { return nil })
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	stdout, stderr, code := runBuildCommand(t, project, "-q", "build")
	if code != 0 || stderr != "" {
		t.Fatalf("build exit = %d, stderr = %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "(2 built, 0 up to date), 1 callbacks, 1 compiled, 1 linked, 0 installed") {
		t.Fatalf("void summary = %q", stdout)
	}
	report := loadBuildReport(t, project)
	if report.Summary == nil || report.Summary.TargetsBuilt != 2 || report.Summary.TargetsUpToDate != 0 ||
		report.Summary.FilesCompiled != 1 || report.Summary.FilesLinked != 1 || report.Summary.Callbacks != 1 ||
		report.Summary.NoOp || !slices.Equal(report.Summary.ChangedFiles, []string{"tasks:src/main.c"}) {
		t.Fatalf("void summary report = %+v", report.Summary)
	}
	if len(report.Targets) != 2 {
		t.Fatalf("void target results = %+v", report.Targets)
	}
	foundTask := false
	for _, target := range report.Targets {
		if target.DurationMs < 0 || target.Status != build.TargetStatusBuilt {
			t.Fatalf("target result = %+v", target)
		}
		if target.Target == "task" {
			foundTask = true
			if target.Kind != string(api.TargetVoid) {
				t.Fatalf("void target kind = %+v", target)
			}
		}
	}
	if !foundTask {
		t.Fatal("void target was not executed")
	}

	stdout, stderr, code = runBuildCommand(t, project, "-q", "build")
	if code != 0 || stderr != "" {
		t.Fatalf("no-op build exit = %d, stderr = %q", code, stderr)
	}
	lines = strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "Build succeeded: no compilation (2 targets, 1 up to date, 1 callbacks,") {
		t.Fatalf("void no-op summary = %q", stdout)
	}
	report = loadBuildReport(t, project)
	if report.Summary == nil || report.Summary.TargetsBuilt != 1 || report.Summary.TargetsUpToDate != 1 ||
		report.Summary.FilesCompiled != 0 || report.Summary.FilesLinked != 0 || report.Summary.Callbacks != 1 ||
		!report.Summary.NoOp || len(report.Summary.ChangedFiles) != 0 {
		t.Fatalf("void no-op summary report = %+v", report.Summary)
	}
	for _, target := range report.Targets {
		want := build.TargetStatusUpToDate
		if target.Target == "task" {
			want = build.TargetStatusBuilt
		}
		if target.Status != want {
			t.Fatalf("target result = %+v, want status %q", target, want)
		}
	}
}

type jsonBuildDocument struct {
	SchemaVersion   int               `json:"schemaVersion"`
	Command         string            `json:"command"`
	Status          string            `json:"status"`
	ReportPath      string            `json:"reportPath"`
	InstallManifest string            `json:"installManifest"`
	Invocation      string            `json:"invocation"`
	Data            buildReport       `json:"data"`
	Diagnostics     []buildDiagnostic `json:"diagnostics"`
}

func parseJSONBuildOutput(t *testing.T, stdout string) jsonBuildDocument {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(stdout))
	var document jsonBuildDocument
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout contains more than one JSON document (%v)\n%s", err, stdout)
	}
	if document.SchemaVersion != 1 {
		t.Fatalf("envelope schemaVersion = %d, want 1", document.SchemaVersion)
	}
	if document.Data.SchemaVersion != 2 {
		t.Fatalf("data schemaVersion = %d, want 2", document.Data.SchemaVersion)
	}
	if document.Command != reportCommandBuild && document.Command != reportCommandRebuild && document.Command != reportCommandTest {
		t.Fatalf("envelope command = %q", document.Command)
	}
	if document.Data.Command != document.Command {
		t.Fatalf("data command = %q, envelope command = %q", document.Data.Command, document.Command)
	}
	if document.Invocation != document.Command {
		t.Fatalf("invocation = %q, envelope command = %q", document.Invocation, document.Command)
	}
	if !filepath.IsAbs(document.ReportPath) || filepath.Base(document.ReportPath) != "vmake-build.json" {
		t.Fatalf("reportPath = %q, want an absolute vmake-build.json path", document.ReportPath)
	}
	if !filepath.IsAbs(document.InstallManifest) || filepath.Base(document.InstallManifest) != "manifest.json" {
		t.Fatalf("installManifest = %q, want an absolute manifest.json path", document.InstallManifest)
	}
	if document.Diagnostics == nil || document.Data.Diagnostics == nil {
		t.Fatalf("diagnostics must be arrays: envelope %#v, data %#v", document.Diagnostics, document.Data.Diagnostics)
	}
	if !slices.Equal(document.Diagnostics, document.Data.Diagnostics) {
		t.Fatalf("envelope diagnostics %+v do not mirror data diagnostics %+v", document.Diagnostics, document.Data.Diagnostics)
	}
	return document
}

func assertNoHumanJSONStdout(t *testing.T, stdout string, phrases ...string) {
	t.Helper()
	for _, phrase := range append([]string{"Build succeeded", "Build failed", "Tests succeeded", "Tests failed"}, phrases...) {
		if strings.Contains(stdout, phrase) {
			t.Fatalf("stdout contains human text %q:\n%s", phrase, stdout)
		}
	}
}

func assertJSONDataMatchesReport(t *testing.T, project string, data buildReport) {
	t.Helper()
	fromFile, err := json.Marshal(loadBuildReport(t, project))
	if err != nil {
		t.Fatal(err)
	}
	fromEnvelope, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fromFile, fromEnvelope) {
		t.Fatalf("envelope data differs from the report file:\nfile:     %s\nenvelope: %s", fromFile, fromEnvelope)
	}
}

func TestBuildJSONSuccessNoopAndScriptOutput(t *testing.T) {
	project := writeBuildSummaryProject(t, "int main(void) { return 0; }\n")
	stdout, stderr, code := runBuildCommand(t, project, "build", "--json")
	if code != 0 {
		t.Fatalf("build --json exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	cold := parseJSONBuildOutput(t, stdout)
	if cold.Command != reportCommandBuild || cold.Status != "ok" || cold.Data.Status != "succeeded" || cold.Data.Error != "" {
		t.Fatalf("cold envelope = %+v / data = %+v", cold, cold.Data)
	}
	if cold.Data.Summary == nil || cold.Data.Summary.TargetsBuilt != 1 || cold.Data.Summary.TargetsUpToDate != 0 ||
		cold.Data.Summary.FilesCompiled != 1 || cold.Data.Summary.FilesLinked != 1 ||
		cold.Data.Summary.Callbacks != 0 || cold.Data.Summary.NoOp ||
		!slices.Equal(cold.Data.Summary.ChangedFiles, []string{"summary:src/main.c"}) {
		t.Fatalf("cold summary = %+v", cold.Data.Summary)
	}
	if !strings.Contains(stderr, "SCRIPT NOISE") {
		t.Fatalf("buildscript print is missing from stderr:\n%s", stderr)
	}
	assertNoHumanJSONStdout(t, stdout, "SCRIPT NOISE")
	assertJSONDataMatchesReport(t, project, cold.Data)

	noopStdout, noopStderr, code := runBuildCommand(t, project, "build", "--json")
	if code != 0 {
		t.Fatalf("no-op build --json exit = %d, stderr = %q", code, noopStderr)
	}
	noop := parseJSONBuildOutput(t, noopStdout)
	if noop.Status != "ok" || noop.Data.Summary == nil ||
		noop.Data.Summary.TargetsBuilt != 0 || noop.Data.Summary.TargetsUpToDate != 1 ||
		noop.Data.Summary.FilesCompiled != 0 || noop.Data.Summary.FilesLinked != 0 ||
		!noop.Data.Summary.NoOp || noop.Data.Summary.Callbacks != 0 ||
		len(noop.Data.Summary.ChangedFiles) != 0 {
		t.Fatalf("no-op envelope = %+v / summary = %+v", noop, noop.Data.Summary)
	}
	if !strings.Contains(noopStderr, "SCRIPT NOISE") {
		t.Fatalf("no-op buildscript print is missing from stderr:\n%s", noopStderr)
	}
	assertNoHumanJSONStdout(t, noopStdout, "SCRIPT NOISE")
	assertJSONDataMatchesReport(t, project, noop.Data)
}

func TestBuildJSONCompileFailure(t *testing.T) {
	project := writeBuildReportProject(t, "int main(void) {\n\tint value = ;\n\treturn value;\n}\n")
	stdout, stderr, code := runBuildCommand(t, project, "build", "--json")
	if code != 1 {
		t.Fatalf("build --json exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	document := parseJSONBuildOutput(t, stdout)
	if document.Command != reportCommandBuild || document.Status != "error" || document.Data.Status != "failed" {
		t.Fatalf("failure envelope = %+v / data = %+v", document, document.Data)
	}
	if document.Data.Error == "" || !strings.Contains(document.Data.Error, "src/main.c:2:") {
		t.Fatalf("concise error = %q", document.Data.Error)
	}
	if len(document.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", document.Diagnostics)
	}
	diagnostic := document.Diagnostics[0]
	if diagnostic.Code != "VMAKE_E_COMPILE" || diagnostic.File != "src/main.c" || diagnostic.Line != 2 || diagnostic.Column == 0 {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	if strings.Count(stderr, "error: expected expression") != 1 {
		t.Fatalf("compiler diagnostic on stderr %d times:\n%s", strings.Count(stderr, "error: expected expression"), stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	if !strings.HasPrefix(lines[len(lines)-1], "Error: src/main.c:2:") {
		t.Fatalf("final stderr line = %q", lines[len(lines)-1])
	}
	assertNoHumanJSONStdout(t, stdout)
	assertJSONDataMatchesReport(t, project, document.Data)
}

func TestTestJSONPassing(t *testing.T) {
	project := writeTestCommandProject(t, passingTestSource)
	stdout, stderr, code := runBuildCommand(t, project, "test", "--json")
	if code != 0 {
		t.Fatalf("test --json exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	document := parseJSONBuildOutput(t, stdout)
	if document.Command != reportCommandTest || document.Status != "ok" || document.Data.Status != "succeeded" {
		t.Fatalf("test envelope = %+v / data = %+v", document, document.Data)
	}
	if len(document.Data.Tests) != 1 {
		t.Fatalf("tests = %+v", document.Data.Tests)
	}
	test := document.Data.Tests[0]
	if test.Name != "tests-project:tests" || test.Package != "tests-project" || test.Target != "tests" ||
		!test.Passed || test.ElapsedMs < 0 || test.ExitCode != 0 {
		t.Fatalf("test entry = %+v", test)
	}
	if document.Data.Summary == nil || document.Data.Summary.TestsFound != 1 ||
		document.Data.Summary.TestsPassed != 1 || document.Data.Summary.TestsFailed != 0 {
		t.Fatalf("test summary = %+v", document.Data.Summary)
	}
	if !strings.Contains(stderr, "1/1 test(s) passed.") {
		t.Fatalf("human test progress is missing from stderr:\n%s", stderr)
	}
	assertNoHumanJSONStdout(t, stdout, "test(s) passed")
	assertJSONDataMatchesReport(t, project, document.Data)
}

func TestTestJSONFailure(t *testing.T) {
	project := writeTestCommandProject(t, failingTestSource)
	stdout, stderr, code := runBuildCommand(t, project, "test", "--json")
	if code != 1 {
		t.Fatalf("test --json exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	document := parseJSONBuildOutput(t, stdout)
	if document.Command != reportCommandTest || document.Status != "error" || document.Data.Status != "failed" {
		t.Fatalf("test failure envelope = %+v / data = %+v", document, document.Data)
	}
	if document.Data.Error != "1 test(s) failed" {
		t.Fatalf("test failure error = %q", document.Data.Error)
	}
	if len(document.Data.Tests) != 1 || document.Data.Tests[0].Passed || document.Data.Tests[0].ExitCode != 1 {
		t.Fatalf("failed test entries = %+v", document.Data.Tests)
	}
	if document.Data.Summary == nil || document.Data.Summary.TestsFound != 1 ||
		document.Data.Summary.TestsPassed != 0 || document.Data.Summary.TestsFailed != 1 {
		t.Fatalf("failed test summary = %+v", document.Data.Summary)
	}
	if !strings.Contains(stderr, "Error: 1 test(s) failed") {
		t.Fatalf("failure error is missing from stderr:\n%s", stderr)
	}
	assertNoHumanJSONStdout(t, stdout, "test(s) passed")
	assertJSONDataMatchesReport(t, project, document.Data)
}

func TestTestReportCapturesPassingOutput(t *testing.T) {
	project := writeTestCommandProject(t, `#include <stdio.h>
int main(void) {
	printf("captured pass output\n");
	return 0;
}
`)
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 0 {
		t.Fatalf("test exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "captured pass output") {
		t.Fatalf("test stdout was not streamed:\n%s", stdout)
	}
	report := loadBuildReport(t, project)
	if len(report.Tests) != 1 {
		t.Fatalf("tests = %+v", report.Tests)
	}
	test := report.Tests[0]
	if !test.Passed || test.ExitCode != 0 || !strings.Contains(test.Output, "captured pass output") {
		t.Fatalf("passing test entry = %+v", test)
	}
	if strings.Contains(test.Output, "PASS tests-project:tests") {
		t.Fatalf("test output captured vmake progress: %q", test.Output)
	}
}

func TestTestReportCapturesFailureOutputAndExitCode(t *testing.T) {
	project := writeTestCommandProject(t, `#include <stdio.h>
int main(void) {
	printf("failing stdout marker\n");
	fprintf(stderr, "failing stderr marker\n");
	return 3;
}
`)
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 1 {
		t.Fatalf("test exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "failing stdout marker") || !strings.Contains(stderr, "failing stderr marker") {
		t.Fatalf("test output was not streamed (stdout %q, stderr %q)", stdout, stderr)
	}
	if !strings.Contains(stderr, "FAIL tests-project:tests") {
		t.Fatalf("FAIL line missing from stderr:\n%s", stderr)
	}
	report := loadBuildReport(t, project)
	if len(report.Tests) != 1 {
		t.Fatalf("tests = %+v", report.Tests)
	}
	test := report.Tests[0]
	if test.Passed || test.ExitCode != 3 {
		t.Fatalf("failed test entry = %+v", test)
	}
	if !strings.Contains(test.Output, "failing stdout marker") || !strings.Contains(test.Output, "failing stderr marker") {
		t.Fatalf("failed test output = %q", test.Output)
	}
}

func TestTestReportTruncatesLongOutput(t *testing.T) {
	project := writeTestCommandProject(t, `#include <stdio.h>
int main(void) {
	for (int i = 0; i < 10000; i++) {
		putchar('x');
	}
	return 1;
}
`)
	_, _, code := runBuildCommand(t, project, "test")
	if code != 1 {
		t.Fatalf("test exit = %d, want 1", code)
	}
	report := loadBuildReport(t, project)
	if len(report.Tests) != 1 || report.Tests[0].ExitCode != 1 {
		t.Fatalf("tests = %+v", report.Tests)
	}
	output := report.Tests[0].Output
	if len(output) > buildTestOutputLimit {
		t.Fatalf("test output is not bounded: %d bytes", len(output))
	}
	if !strings.HasPrefix(output, buildTestOutputMarker) {
		t.Fatalf("test output has no truncation marker: %q", output[:min(len(output), 64)])
	}
	raw, err := os.ReadFile(reportPath(project))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 16*1024 {
		t.Fatalf("report with one test output is %d bytes", len(raw))
	}
}

func TestTestBuildFailureKeepsCompletedTargets(t *testing.T) {
	project := writeTestCommandProject(t, "int main(void) {\n\tint value = ;\n\treturn value;\n}\n")
	stdout, stderr, code := runBuildCommand(t, project, "test")
	if code != 1 {
		t.Fatalf("test exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Tests failed: tests/test_app.c:2:") {
		t.Fatalf("build-phase failure summary missing:\n%s", stdout)
	}
	report := loadBuildReport(t, project)
	if report.Status != "failed" || report.Command != reportCommandTest {
		t.Fatalf("build-phase failure report = %+v", report)
	}
	if len(report.Targets) != 1 || report.Targets[0].Target != "app" || report.Targets[0].Status != build.TargetStatusBuilt {
		t.Fatalf("completed targets = %+v", report.Targets)
	}
	summary := report.Summary
	if summary == nil || summary.TargetsTotal != 1 || summary.TargetsBuilt != 1 ||
		summary.TargetsFailed != 1 || summary.FilesLinked != 1 || summary.FilesCompiled < 1 ||
		summary.ArtifactsInstalled != 0 {
		t.Fatalf("partial summary = %+v", summary)
	}
	if report.Artifacts == nil || len(report.Artifacts) != 0 || len(report.Tests) != 0 {
		t.Fatalf("build-phase failure results = %+v (tests %+v)", report.Artifacts, report.Tests)
	}
	if report.CompileDatabase == "" {
		t.Fatal("build-phase failure lost the compile database")
	}
}

func TestTestJSONNoTargets(t *testing.T) {
	project := writeTestCommandProject(t, "")
	stdout, stderr, code := runBuildCommand(t, project, "test", "--json")
	if code != 1 {
		t.Fatalf("test --json exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	document := parseJSONBuildOutput(t, stdout)
	if document.Status != "error" || document.Data.Status != "failed" || document.Data.Error != "no test targets found" {
		t.Fatalf("no-tests envelope = %+v / data = %+v", document, document.Data)
	}
	if len(document.Data.Tests) != 0 {
		t.Fatalf("no-tests entries = %+v", document.Data.Tests)
	}
	if document.Data.Summary == nil || document.Data.Summary.TestsFound != 0 {
		t.Fatalf("no-tests summary = %+v", document.Data.Summary)
	}
	if !strings.Contains(stderr, "Error: no test targets found") {
		t.Fatalf("no-tests error is missing from stderr:\n%s", stderr)
	}
	assertNoHumanJSONStdout(t, stdout, "test(s) passed")
	assertJSONDataMatchesReport(t, project, document.Data)
}

func TestRebuildJSONSmoke(t *testing.T) {
	project := writeBuildSummaryProject(t, "int main(void) { return 0; }\n")
	stdout, stderr, code := runBuildCommand(t, project, "rebuild", "--json")
	if code != 0 {
		t.Fatalf("rebuild --json exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	document := parseJSONBuildOutput(t, stdout)
	if document.Command != reportCommandRebuild || document.Invocation != reportCommandRebuild ||
		document.Data.Command != reportCommandRebuild ||
		document.Status != "ok" || document.Data.Status != "succeeded" {
		t.Fatalf("rebuild envelope = %+v / data = %+v", document, document.Data)
	}
	if document.Data.Summary == nil || document.Data.Summary.TargetsBuilt != 1 || document.Data.Summary.FilesCompiled != 1 {
		t.Fatalf("rebuild summary = %+v", document.Data.Summary)
	}
	if document.ReportPath != reportPath(project) {
		t.Fatalf("rebuild reportPath = %q, want %q", document.ReportPath, reportPath(project))
	}
	if document.InstallManifest != filepath.Join(project, "install", "manifest.json") {
		t.Fatalf("rebuild installManifest = %q", document.InstallManifest)
	}
	if !strings.Contains(stderr, "SCRIPT NOISE") {
		t.Fatalf("rebuild buildscript print is missing from stderr:\n%s", stderr)
	}
	assertNoHumanJSONStdout(t, stdout, "SCRIPT NOISE")
	assertJSONDataMatchesReport(t, project, document.Data)
}

func countWarningLines(output, marker string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "warning:") && strings.Contains(line, marker) {
			count++
		}
	}
	return count
}

func writeBuildWarningProject(t *testing.T, source string) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "warning")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c").AddCFlags("-Wno-error")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), source)
	return project
}

func TestBuildReportSuccessWarnings(t *testing.T) {
	project := writeBuildWarningProject(t, "static int vmake_warning_probe;\n\nint main(void) { return 0; }\n")
	stdout, stderr, code := runBuildCommand(t, project, "build")
	if code != 0 {
		t.Fatalf("warning build exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Build succeeded") {
		t.Fatalf("success summary missing:\n%s", stdout)
	}
	if countWarningLines(stderr, "vmake_warning_probe") != 1 {
		t.Fatalf("compiler warning on stderr %d times:\n%s", countWarningLines(stderr, "vmake_warning_probe"), stderr)
	}
	report := loadBuildReport(t, project)
	if report.Status != "succeeded" || report.Error != "" {
		t.Fatalf("warning report = %+v", report)
	}
	if len(report.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want one warning", report.Diagnostics)
	}
	warning := report.Diagnostics[0]
	if warning.Severity != "warning" || warning.Code != buildWarningCode || filepath.Base(warning.File) != "main.c" ||
		warning.Line <= 0 || !strings.Contains(warning.Message, "vmake_warning_probe") {
		t.Fatalf("warning diagnostic = %+v", warning)
	}
	noopStdout, noopStderr, code := runBuildCommand(t, project, "-q", "build")
	if code != 0 || noopStderr != "" {
		t.Fatalf("no-op build exit = %d, stderr = %q", code, noopStderr)
	}
	if !strings.HasPrefix(noopStdout, "Build succeeded: no compilation") {
		t.Fatalf("no-op stdout = %q", noopStdout)
	}
	report = loadBuildReport(t, project)
	if report.Diagnostics == nil || len(report.Diagnostics) != 0 {
		t.Fatalf("clean no-op diagnostics = %#v, want []", report.Diagnostics)
	}
}

func TestBuildJSONQuietWarningDiagnostics(t *testing.T) {
	project := writeBuildWarningProject(t, "static int vmake_warning_probe;\n\nint main(void) { return 0; }\n")
	stdout, stderr, code := runBuildCommand(t, project, "-q", "build", "--json")
	if code != 0 {
		t.Fatalf("quiet JSON build exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	document := parseJSONBuildOutput(t, stdout)
	if document.Status != "ok" || document.Data.Status != "succeeded" || document.Data.Error != "" {
		t.Fatalf("quiet warning envelope = %+v / data = %+v", document, document.Data)
	}
	if len(document.Diagnostics) != 1 || len(document.Data.Diagnostics) != 1 {
		t.Fatalf("quiet warning diagnostics = %+v", document.Diagnostics)
	}
	warning := document.Diagnostics[0]
	if warning.Severity != "warning" || warning.Code != buildWarningCode ||
		filepath.Base(warning.File) != "main.c" || !strings.Contains(warning.Message, "vmake_warning_probe") {
		t.Fatalf("quiet warning diagnostic = %+v", warning)
	}
	if countWarningLines(stderr, "vmake_warning_probe") != 1 {
		t.Fatalf("compiler warning streamed %d times:\n%s", countWarningLines(stderr, "vmake_warning_probe"), stderr)
	}
	assertNoHumanJSONStdout(t, stdout)
	assertJSONDataMatchesReport(t, project, document.Data)
}

func TestBuildJSONEnvelopePaths(t *testing.T) {
	wantManifest := func(project string) string { return filepath.Join(project, "install", "manifest.json") }

	project := writeBuildReportProject(t, "int main(void) { return 0; }\n")
	stdout, stderr, code := runBuildCommand(t, project, "build", "--json")
	if code != 0 {
		t.Fatalf("success build --json exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	success := parseJSONBuildOutput(t, stdout)
	if success.ReportPath != reportPath(project) || success.InstallManifest != wantManifest(project) ||
		success.Invocation != reportCommandBuild {
		t.Fatalf("success paths = reportPath %q, installManifest %q, invocation %q", success.ReportPath, success.InstallManifest, success.Invocation)
	}
	if len(success.Diagnostics) != 0 {
		t.Fatalf("clean build diagnostics = %+v, want []", success.Diagnostics)
	}
	assertJSONDataMatchesReport(t, project, success.Data)

	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) {\n\tint value = ;\n\treturn value;\n}\n")
	stdout, stderr, code = runBuildCommand(t, project, "build", "--json")
	if code != 1 {
		t.Fatalf("failure build --json exit = %d, want 1 (stderr %q)\n%s", code, stderr, stdout)
	}
	failure := parseJSONBuildOutput(t, stdout)
	if failure.ReportPath != reportPath(project) || failure.InstallManifest != wantManifest(project) ||
		failure.Invocation != reportCommandBuild || failure.Status != "error" {
		t.Fatalf("failure paths = %+v", failure)
	}
	assertJSONDataMatchesReport(t, project, failure.Data)

	testProject := writeTestCommandProject(t, passingTestSource)
	stdout, stderr, code = runBuildCommand(t, testProject, "test", "--json")
	if code != 0 {
		t.Fatalf("test --json exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	testDocument := parseJSONBuildOutput(t, stdout)
	if testDocument.ReportPath != reportPath(testProject) ||
		testDocument.InstallManifest != wantManifest(testProject) ||
		testDocument.Invocation != reportCommandTest {
		t.Fatalf("test paths = reportPath %q, installManifest %q, invocation %q",
			testDocument.ReportPath, testDocument.InstallManifest, testDocument.Invocation)
	}
	assertJSONDataMatchesReport(t, testProject, testDocument.Data)
}

func TestBuildReportFailureKeepsWarningsAfterErrors(t *testing.T) {
	project := filepath.Join(t.TempDir(), "warning-failure")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("warned").SetKind(api.TargetBinary).AddFiles("src/warned.c").AddCFlags("-Wno-error")
		ctx.Target("broken").SetKind(api.TargetBinary).AddFiles("src/broken.c").AddDeps("warned")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "warned.c"), "static int vmake_warning_probe;\n\nint main(void) { return 0; }\n")
	writeExtensionFile(t, filepath.Join(project, "src", "broken.c"), "int main(void) {\n\tint value = ;\n\treturn value;\n}\n")
	stdout, stderr, code := runBuildCommand(t, project, "build")
	if code != 1 {
		t.Fatalf("warning failure exit = %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if countWarningLines(stderr, "vmake_warning_probe") != 1 {
		t.Fatalf("compiler warning on stderr %d times:\n%s", countWarningLines(stderr, "vmake_warning_probe"), stderr)
	}
	report := loadBuildReport(t, project)
	if report.Status != "failed" || len(report.Diagnostics) != 2 {
		t.Fatalf("warning failure report = %+v", report)
	}
	if report.Diagnostics[0].Severity != "error" || report.Diagnostics[0].Code != "VMAKE_E_COMPILE" ||
		filepath.Base(report.Diagnostics[0].File) != "broken.c" {
		t.Fatalf("first diagnostic is not the error: %+v", report.Diagnostics[0])
	}
	warning := report.Diagnostics[1]
	if warning.Severity != "warning" || warning.Code != buildWarningCode ||
		filepath.Base(warning.File) != "warned.c" || !strings.Contains(warning.Message, "vmake_warning_probe") {
		t.Fatalf("warning diagnostic = %+v", warning)
	}
	if !strings.Contains(report.Error, "broken.c:2:") {
		t.Fatalf("failure summary = %q", report.Error)
	}
}

func TestParseBuildWarnings(t *testing.T) {
	output := strings.Join([]string{
		"src/main.c:8:12: warning: unused variable 'x' [-Wunused-variable]",
		"src/main.c:8:12: warning: unused variable 'x' [-Wunused-variable]",
		"src/main.c:9: warning: warning without a column",
		"warning: generic warning\r",
		"ld: warning: cannot find entry symbol _start",
		`C:\project\src\main.c:12:3: warning: windows path warning`,
		"src/main.c:10:5: error: not a warning",
		"note: not a warning either",
		"CMake Warning at CMakeLists.txt:12 (message):",
	}, "\n")
	warnings := parseBuildWarnings(output)
	if len(warnings) != 5 {
		t.Fatalf("warnings = %+v", warnings)
	}
	for _, warning := range warnings {
		if warning.Severity != "warning" || warning.Code != buildWarningCode {
			t.Fatalf("warning entry = %+v", warning)
		}
	}
	located := warnings[0]
	if located.File != "src/main.c" || located.Line != 8 || located.Column != 12 ||
		located.Message != "unused variable 'x' [-Wunused-variable]" {
		t.Fatalf("located warning = %+v", located)
	}
	if warnings[1].File != "src/main.c" || warnings[1].Line != 9 || warnings[1].Column != 0 {
		t.Fatalf("columnless warning = %+v", warnings[1])
	}
	if warnings[2].File != "" || warnings[2].Message != "generic warning" {
		t.Fatalf("generic warning = %+v", warnings[2])
	}
	if warnings[3].File != "" || warnings[3].Message != "ld: cannot find entry symbol _start" {
		t.Fatalf("tool warning = %+v", warnings[3])
	}
	if warnings[4].File != `C:\project\src\main.c` || warnings[4].Line != 12 || warnings[4].Column != 3 {
		t.Fatalf("windows warning = %+v", warnings[4])
	}
}

func TestParseBuildWarningsCapped(t *testing.T) {
	var output strings.Builder
	for i := 0; i < buildWarningLimit+10; i++ {
		fmt.Fprintf(&output, "src/w%d.c:1: warning: warning number %d\n", i, i)
	}
	output.WriteString("src/w0.c:1: warning: warning number 0\n")
	warnings := parseBuildWarnings(output.String())
	if len(warnings) != buildWarningLimit {
		t.Fatalf("warnings = %d, want %d", len(warnings), buildWarningLimit)
	}
}

func TestBuildStderrCaptureKeepsBoundedTail(t *testing.T) {
	capture := &buildStderrCapture{}
	capture.append(bytes.Repeat([]byte("x"), buildStderrCaptureSize))
	capture.append([]byte("\nsrc/main.c:4:2: warning: retained tail\n"))
	if len(capture.buffer) != buildStderrCaptureSize {
		t.Fatalf("capture length = %d, want %d", len(capture.buffer), buildStderrCaptureSize)
	}
	warnings := capture.warnings()
	if len(warnings) != 1 || warnings[0].File != "src/main.c" || warnings[0].Line != 4 {
		t.Fatalf("tail warnings = %+v", warnings)
	}
}

func TestMergeBuildDiagnosticsKeepsErrorsFirst(t *testing.T) {
	diagnostics := []buildDiagnostic{
		{Severity: "error", Code: "VMAKE_E_COMPILE", File: "src/broken.c", Line: 2, Column: 1, Message: "boom", Package: "pkg", Target: "pkg:broken"},
		{Severity: "warning", Code: "VMAKE_E_COMPILE", File: "src/warned.c", Line: 3, Column: 7, Message: "warned", Package: "pkg", Target: "pkg:warned"},
	}
	warnings := []buildDiagnostic{
		{Severity: "warning", Code: buildWarningCode, File: "src/warned.c", Line: 3, Column: 7, Message: "warned"},
		{Severity: "warning", Code: buildWarningCode, Message: "generic warning"},
	}
	merged := mergeBuildDiagnostics(diagnostics, warnings)
	if len(merged) != 3 {
		t.Fatalf("merged = %+v", merged)
	}
	if merged[0].Severity != "error" || merged[0].Code != "VMAKE_E_COMPILE" {
		t.Fatalf("error moved: %+v", merged[0])
	}
	if merged[1].Severity != "warning" || merged[1].Code != buildWarningCode ||
		merged[1].Target != "pkg:warned" {
		t.Fatalf("merged warning = %+v", merged[1])
	}
	if merged[2].Message != "generic warning" {
		t.Fatalf("generic warning = %+v", merged[2])
	}
}
