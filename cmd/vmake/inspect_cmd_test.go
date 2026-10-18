package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestInspectBootstrapChild(t *testing.T) {
	dir := os.Getenv("VMAKE_TEST_INSPECT_DIR")
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

func runInspectCommand(t *testing.T, projectDir string, args ...string) (string, string, int) {
	t.Helper()
	return runInspectCommandWithState(t, projectDir, t.TempDir(), args...)
}

func runInspectCommandWithState(t *testing.T, projectDir, vmakeDir string, args ...string) (string, string, int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commandArgs := append([]string{"-test.run=^TestInspectBootstrapChild$", "--"}, args...)
	cmd := exec.Command(exe, commandArgs...)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(),
		"VMAKE_TEST_INSPECT_DIR="+vmakeDir,
		"VMAKE_CACHE="+t.TempDir(),
		"VMAKE_TRUST_ALL=1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run inspect: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func decodeInspectEnvelope(t *testing.T, stdout string) *inspectEnvelope {
	t.Helper()
	var envelope inspectEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	return &envelope
}

func inspectPackageNamed(t *testing.T, data *inspectData, name string) *inspectPackage {
	t.Helper()
	for i := range data.Packages {
		if data.Packages[i].Name == name {
			return &data.Packages[i]
		}
	}
	t.Fatalf("package %q missing from inspect model", name)
	return nil
}

func inspectTargetNamed(t *testing.T, pkg *inspectPackage, name string) *inspectTarget {
	t.Helper()
	for i := range pkg.Targets {
		if pkg.Targets[i].Name == name {
			return &pkg.Targets[i]
		}
	}
	t.Fatalf("target %q missing from package %q", name, pkg.Name)
	return nil
}

func writeInspectProject(t *testing.T, dir string) string {
	t.Helper()
	project := filepath.Join(dir, "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("with_dep").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Link the dependency")
		ctx.Option("retries").
			SetType(api.OptionInt).
			SetDefault(3)
		ctx.Option("label").
			SetType(api.OptionString).
			SetDefault("default")
	})
	p.OnRequire(func(ctx *api.RequireContext) {
		if ctx.When("with_dep", true) {
			ctx.AddRequires("dep")
		}
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		target := ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddIncludes("include").
			AddPublicIncludes("include").
			AddDefines("APP_DEFINE=1")
		if ctx.When("with_dep", true) {
			target.AddDeps("dep:deplib")
		}
		ctx.Target("docs").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error { return nil })
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	writeExtensionFile(t, filepath.Join(project, "include", "app.h"), "#define APP 1\n")
	writeExtensionFile(t, filepath.Join(project, "dep", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("dep_mode").
			SetType(api.OptionChoice).
			SetValues("fast", "slow").
			SetDefault("fast")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("deplib").
			SetKind(api.TargetStatic).
			AddFiles("src/*.c").
			AddPublicIncludes("include")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "dep", "src", "lib.c"), "int dep(void);\nint dep(void) { return 1; }\n")
	writeExtensionFile(t, filepath.Join(project, "dep", "include", "dep.h"), "#define DEP 1\n")
	return project
}

func writeInspectFreshnessProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	return project
}

func writeInspectConfig(t *testing.T, project string) string {
	t.Helper()
	configPath := filepath.Join(project, ".vmake", "config.json")
	writeExtensionFile(t, configPath, `{"version":"1"}`)
	digest, _, err := fileDigest(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func writeInspectBuildReport(t *testing.T, project, finished, configSHA256 string) {
	t.Helper()
	writeExtensionFile(t, filepath.Join(project, "build", "vmake-build.json"), fmt.Sprintf(
		`{"schemaVersion":2,"buildId":"freshness","status":"succeeded","command":"build","started":"2020-01-01T00:00:00Z","finished":%q,"configSha256":%q}`,
		finished, configSHA256))
}

func setInspectSourceTime(t *testing.T, paths []string, modTime time.Time) {
	t.Helper()
	for _, path := range paths {
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInspectProjectModel(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %s", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("successful inspect wrote stderr: %q", stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.SchemaVersion != inspectSchemaVersion || envelope.Command != "inspect" || envelope.Status != "ok" {
		t.Fatalf("envelope = %+v", envelope)
	}
	if envelope.Data == nil {
		t.Fatal("successful inspect has no data")
	}
	if envelope.Diagnostics == nil || len(envelope.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want []", envelope.Diagnostics)
	}
	data := envelope.Data
	if data.Root != project || data.ConfigFile != "config.json" || data.Mode != "debug" || data.Toolchain != "host" {
		t.Fatalf("data header = %+v", data)
	}
	if want := filepath.Join(project, "build", "compile_commands.json"); data.CompileDatabase != want {
		t.Errorf("compileDatabase = %q, want %q", data.CompileDatabase, want)
	}
	if want := filepath.Join(project, "build", "vmake-build.json"); data.BuildReport != want {
		t.Errorf("buildReport = %q, want %q", data.BuildReport, want)
	}
	if want := filepath.Join(project, "install"); data.InstallPrefix != want {
		t.Errorf("installPrefix = %q, want %q", data.InstallPrefix, want)
	}
	if want := filepath.Join(project, "install", "manifest.json"); data.InstallManifest != want {
		t.Errorf("installManifest = %q, want %q", data.InstallManifest, want)
	}
	if !slices.Equal(data.Roots, []string{"proj"}) {
		t.Errorf("roots = %v", data.Roots)
	}
	if data.ToolchainInfo == nil || data.ToolchainInfo.Name != "host" || data.ToolchainInfo.CC == "" {
		t.Errorf("toolchainInfo = %+v", data.ToolchainInfo)
	}
	if data.Global["mode"] != "debug" || data.Global["toolchain"] != "host" {
		t.Errorf("global = %v", data.Global)
	}
	if data.Overrides != nil {
		t.Errorf("unexpected overrides: %v", data.Overrides)
	}
	if !slices.Equal(data.Graph, []inspectEdge{{From: "proj", To: "dep"}}) {
		t.Errorf("graph = %v", data.Graph)
	}

	proj := inspectPackageNamed(t, data, "proj")
	if !proj.Needed || proj.Kind != "local" || proj.Path != "." || proj.Toolchain != "host" {
		t.Errorf("root package = %+v", proj)
	}
	if !slices.Equal(proj.Deps, []string{"dep"}) {
		t.Errorf("root deps = %v", proj.Deps)
	}
	if proj.Options["with_dep"] != true || proj.Options["retries"] != float64(3) || proj.Options["label"] != "default" {
		t.Errorf("root options = %v", proj.Options)
	}
	app := inspectTargetNamed(t, proj, "app")
	if app.Kind != "binary" || !app.Default || app.Test || app.Output != "app" {
		t.Errorf("app target = %+v", app)
	}
	if app.InstallPath != "bin/app" {
		t.Errorf("app installPath = %q, want bin/app", app.InstallPath)
	}
	if app.Installs != nil {
		t.Errorf("app installs = %+v, want none", app.Installs)
	}
	if !slices.Equal(app.Files, []string{"src/*.c"}) || !slices.Equal(app.Includes, []string{"include"}) ||
		!slices.Equal(app.PublicIncludes, []string{"include"}) || !slices.Equal(app.Defines, []string{"APP_DEFINE=1"}) ||
		!slices.Equal(app.Deps, []string{"dep:deplib"}) {
		t.Errorf("app target fields = %+v", app)
	}
	docs := inspectTargetNamed(t, proj, "docs")
	if docs.Kind != "void" || docs.Output != "" || docs.Files != nil || docs.InstallPath != "" {
		t.Errorf("docs target = %+v", docs)
	}

	dep := inspectPackageNamed(t, data, "dep")
	if !dep.Needed || dep.Kind != "local" || dep.Path != "dep" || dep.Toolchain != "host" {
		t.Errorf("dep package = %+v", dep)
	}
	if dep.Options["dep_mode"] != "fast" {
		t.Errorf("dep options = %v", dep.Options)
	}
	deplib := inspectTargetNamed(t, dep, "deplib")
	if deplib.Kind != "static" || deplib.Output != "libdeplib.a" || !slices.Equal(deplib.PublicIncludes, []string{"include"}) {
		t.Errorf("deplib target = %+v", deplib)
	}
	if deplib.InstallPath != "lib/libdeplib.a" {
		t.Errorf("deplib installPath = %q, want lib/libdeplib.a", deplib.InstallPath)
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("consecutive inspect runs are not byte-identical")
	}
	for _, dir := range []string{"build", "install"} {
		if _, err := os.Stat(filepath.Join(project, dir)); !os.IsNotExist(err) {
			t.Fatalf("inspect created %s: %v", dir, err)
		}
	}
}

func TestInspectBuildStateAndPackageDirs(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())

	checkAbsent := func(t *testing.T) *inspectData {
		t.Helper()
		stdout, stderr, code := runInspectCommand(t, project, "inspect")
		if code != 0 {
			t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
		}
		envelope := decodeInspectEnvelope(t, stdout)
		if envelope.Status != "ok" || envelope.Data == nil {
			t.Fatalf("envelope = %+v", envelope)
		}
		if len(envelope.Diagnostics) != 0 {
			t.Fatalf("diagnostics = %+v, want none", envelope.Diagnostics)
		}
		data := envelope.Data
		if data.CompileDatabasePresent || data.BuildReportPresent || data.BuildReportState != nil {
			t.Fatalf("build state present before a build: db=%v report=%v state=%+v",
				data.CompileDatabasePresent, data.BuildReportPresent, data.BuildReportState)
		}
		for _, want := range []struct {
			pkg  string
			base string
		}{{"proj", project}, {"dep", filepath.Join(project, "dep")}} {
			pkg := inspectPackageNamed(t, data, want.pkg)
			if !pkg.Needed || pkg.BuildDir == "" || !filepath.IsAbs(pkg.BuildDir) {
				t.Fatalf("%s buildDir = %q (needed %v)", want.pkg, pkg.BuildDir, pkg.Needed)
			}
			if !strings.HasPrefix(pkg.BuildDir, filepath.Join(want.base, "build")+string(filepath.Separator)) {
				t.Fatalf("%s buildDir = %q, want a path under %s", want.pkg, pkg.BuildDir, filepath.Join(want.base, "build"))
			}
			if pkg.BuildKey == "" || pkg.BuildKey != filepath.Base(pkg.BuildDir) {
				t.Fatalf("%s buildKey = %q, buildDir = %q", want.pkg, pkg.BuildKey, pkg.BuildDir)
			}
			if _, err := os.Stat(pkg.BuildDir); !os.IsNotExist(err) {
				t.Fatalf("inspect materialized %s build dir %s: %v", want.pkg, pkg.BuildDir, err)
			}
		}
		return data
	}
	checkAbsent(t)

	if _, stderr, code := runInspectCommand(t, project, "build"); code != 0 {
		t.Fatalf("build exit = %d, stderr = %q", code, stderr)
	}
	rawReport, err := os.ReadFile(filepath.Join(project, "build", "vmake-build.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		BuildID string          `json:"buildId"`
		Status  string          `json:"status"`
		Summary json.RawMessage `json:"summary"`
	}
	if err := json.Unmarshal(rawReport, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "succeeded" || report.BuildID == "" || len(report.Summary) == 0 {
		t.Fatalf("build report = %+v", report)
	}

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect after build exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", envelope.Diagnostics)
	}
	data := envelope.Data
	if !data.CompileDatabasePresent || !data.BuildReportPresent {
		t.Fatalf("presence after build: db=%v report=%v", data.CompileDatabasePresent, data.BuildReportPresent)
	}
	state := data.BuildReportState
	if state == nil {
		t.Fatal("buildReportState is absent after a successful build")
	}
	if state.Status != "succeeded" || state.BuildID != report.BuildID || state.Command != "build" {
		t.Fatalf("buildReportState = %+v, report = %+v", state, report)
	}
	if state.Started == "" || state.Finished == "" || state.Error != "" {
		t.Fatalf("buildReportState identity = %+v", state)
	}
	if !state.ConfigMatches {
		t.Fatal("buildReportState.configMatches = false for the build's own configuration")
	}
	var wantSummary, gotSummary any
	if err := json.Unmarshal(report.Summary, &wantSummary); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(state.Summary, &gotSummary); err != nil {
		t.Fatalf("buildReportState.summary = %s: %v", state.Summary, err)
	}
	if !reflect.DeepEqual(gotSummary, wantSummary) {
		t.Fatalf("buildReportState.summary = %s, report summary = %s", state.Summary, report.Summary)
	}
	for _, name := range []string{"proj", "dep"} {
		pkg := inspectPackageNamed(t, data, name)
		if pkg.BuildDir == "" || pkg.BuildKey == "" {
			t.Fatalf("%s buildDir/buildKey missing after build: %+v", name, pkg)
		}
		info, err := os.Stat(pkg.BuildDir)
		if err != nil || !info.IsDir() {
			t.Fatalf("%s buildDir %q is not a directory: %v", name, pkg.BuildDir, err)
		}
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("consecutive inspect runs after a build are not byte-identical")
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect", "--set", "proj/with_dep=false")
	if code != 0 || stderr != "" {
		t.Fatalf("hypothetical inspect exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data.BuildReportState == nil || data.BuildReportState.ConfigMatches {
		t.Fatalf("--set buildReportState = %+v, want configMatches false", data.BuildReportState)
	}
	if dep := inspectPackageNamed(t, data, "dep"); dep.Needed || dep.BuildDir != "" || dep.BuildKey != "" {
		t.Fatalf("pruned package kept build fields: %+v", dep)
	}

	if stdout, stderr, code := runInspectCommand(t, project, "clean"); code != 0 {
		t.Fatalf("clean exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	data = checkAbsent(t)
	for _, name := range []string{"proj", "dep"} {
		pkg := inspectPackageNamed(t, data, name)
		if _, err := os.Stat(pkg.BuildDir); !os.IsNotExist(err) {
			t.Fatalf("%s build dir survived clean: %q", name, pkg.BuildDir)
		}
	}
}

func TestInspectBuildDirFields(t *testing.T) {
	key := "debug-abc"
	local := &api.PkgDirs{SourceDir: "/src/lib", BuildDir: filepath.Join("/src/lib", "build", key)}
	dir, buildKey := inspectBuildDirFields(local, true)
	if dir != local.BuildDir || buildKey != key {
		t.Fatalf("local fields = %q/%q", dir, buildKey)
	}
	if dir, buildKey := inspectBuildDirFields(local, false); dir != local.BuildDir || buildKey != "" {
		t.Fatalf("remote-like fields = %q/%q", dir, buildKey)
	}
	remote := &api.PkgDirs{SourceDir: "/deps/owner/src", BuildDir: filepath.Join("/deps/owner/out", "member", key, "build")}
	if dir, buildKey := inspectBuildDirFields(remote, false); dir != remote.BuildDir || buildKey != "" {
		t.Fatalf("remote fields = %q/%q", dir, buildKey)
	}
	odd := &api.PkgDirs{BuildDir: filepath.Join("/tmp", key)}
	if _, buildKey := inspectBuildDirFields(odd, true); buildKey != "" {
		t.Fatalf("non-layout build dir got buildKey %q", buildKey)
	}
	if dir, buildKey := inspectBuildDirFields(nil, true); dir != "" || buildKey != "" {
		t.Fatalf("nil dirs = %q/%q", dir, buildKey)
	}
	if dir, buildKey := inspectBuildDirFields(&api.PkgDirs{SourceDir: "/src"}, true); dir != "" || buildKey != "" {
		t.Fatalf("empty build dir = %q/%q", dir, buildKey)
	}
}

func TestInspectBuildReportConfigMatch(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) { p.SetRoot(true) }
`)
	configPath := filepath.Join(project, ".vmake", "config.json")
	writeExtensionFile(t, configPath, `{"version":"1","global":{"mode":"release"}}`)
	digest, _, err := fileDigest(configPath)
	if err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(project, "build", "vmake-build.json")
	writeReport := func(configSHA256 string) {
		t.Helper()
		writeExtensionFile(t, reportPath, `{"schemaVersion":2,"buildId":"abc123","status":"succeeded","command":"build","started":"2026-01-01T00:00:00Z","finished":"2026-01-01T00:00:01Z","configSha256":"`+configSHA256+`","summary":{"targetsTotal":1,"targetsBuilt":1}}`)
	}

	writeReport(digest)
	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	state := data.BuildReportState
	if state == nil || !state.ConfigMatches || state.BuildID != "abc123" || state.Status != "succeeded" {
		t.Fatalf("matching report state = %+v", state)
	}
	if string(state.Summary) == "" {
		t.Fatal("matching report state lost the summary")
	}

	writeReport("not-the-active-digest")
	stdout, stderr, code = runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data.BuildReportState == nil || data.BuildReportState.ConfigMatches {
		t.Fatalf("mismatched report state = %+v, want configMatches false", data.BuildReportState)
	}
}

func TestInspectFreshnessWithoutReport(t *testing.T) {
	project := writeInspectFreshnessProject(t)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	if data == nil || data.BuildReportPresent || data.BuildReportState != nil {
		t.Fatalf("build report state without a report: %+v", data)
	}
	freshness := data.Freshness
	if freshness == nil || freshness.Known {
		t.Fatalf("freshness = %+v, want present with known false", freshness)
	}
	if freshness.ReportStatus != "" || freshness.ReportFinished != "" || freshness.ConfigMatches != nil ||
		freshness.SourceChanged != nil || freshness.ChangedSources != nil || freshness.Truncated != nil {
		t.Fatalf("unknown freshness exposed report/source fields: %+v", freshness)
	}
}

func TestInspectFreshnessUnparsableFinish(t *testing.T) {
	project := writeInspectFreshnessProject(t)
	digest := writeInspectConfig(t, project)

	for _, finished := range []string{"", "not-a-timestamp"} {
		writeInspectBuildReport(t, project, finished, digest)
		stdout, stderr, code := runInspectCommand(t, project, "inspect")
		if code != 0 || stderr != "" {
			t.Fatalf("inspect exit = %d, stderr = %q (finished %q)", code, stderr, finished)
		}
		data := decodeInspectEnvelope(t, stdout).Data
		if data == nil || data.BuildReportState == nil {
			t.Fatalf("parsable report without state: %+v (finished %q)", data, finished)
		}
		freshness := data.Freshness
		if freshness == nil || freshness.Known {
			t.Fatalf("freshness = %+v, want known false (finished %q)", freshness, finished)
		}
		if freshness.ReportStatus != "succeeded" || freshness.ConfigMatches == nil || !*freshness.ConfigMatches {
			t.Fatalf("freshness identity = %+v (finished %q)", freshness, finished)
		}
		if freshness.ReportFinished != "" || freshness.SourceChanged != nil ||
			freshness.ChangedSources != nil || freshness.Truncated != nil {
			t.Fatalf("unparsable finish exposed source fields: %+v (finished %q)", freshness, finished)
		}
	}
}

func TestInspectFreshnessAfterBuildAndSourceEdit(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())
	if _, stderr, code := runBuildCommand(t, project, "build"); code != 0 {
		t.Fatalf("build exit = %d, stderr = %q", code, stderr)
	}
	report := loadBuildReport(t, project)
	finished, err := time.Parse(time.RFC3339Nano, report.Finished)
	if err != nil {
		t.Fatalf("report finished %q: %v", report.Finished, err)
	}

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	freshness := data.Freshness
	if freshness == nil || !freshness.Known {
		t.Fatalf("freshness after build = %+v", freshness)
	}
	if freshness.ReportStatus != report.Status || freshness.ReportFinished != report.Finished {
		t.Fatalf("freshness report identity = %+v, report = %+v", freshness, report)
	}
	if freshness.ConfigMatches == nil || !*freshness.ConfigMatches {
		t.Fatalf("freshness configMatches = %+v, want true", freshness.ConfigMatches)
	}
	if freshness.SourceChanged == nil || *freshness.SourceChanged {
		t.Fatalf("freshness sourceChanged = %+v, want false", freshness.SourceChanged)
	}
	if len(freshness.ChangedSources) != 0 {
		t.Fatalf("changedSources after build = %v, want none", freshness.ChangedSources)
	}
	if freshness.Truncated == nil || *freshness.Truncated {
		t.Fatalf("freshness truncated = %+v, want false", freshness.Truncated)
	}

	edited := finished.Add(2 * time.Second)
	for _, rel := range []string{"src/main.c", "dep/src/lib.c"} {
		writeExtensionFile(t, filepath.Join(project, filepath.FromSlash(rel)), "int edited(void) { return 1; }\n")
	}
	setInspectSourceTime(t, []string{filepath.Join(project, "src", "main.c"), filepath.Join(project, "dep", "src", "lib.c")}, edited)

	stdout, stderr, code = runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect after edit exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	freshness = data.Freshness
	if freshness == nil || !freshness.Known {
		t.Fatalf("freshness after edit = %+v", freshness)
	}
	if freshness.ConfigMatches == nil || !*freshness.ConfigMatches {
		t.Fatalf("source edit changed configMatches: %+v", freshness.ConfigMatches)
	}
	if freshness.SourceChanged == nil || !*freshness.SourceChanged {
		t.Fatalf("sourceChanged after edit = %+v, want true", freshness.SourceChanged)
	}
	if want := []string{"dep:src/lib.c", "proj:src/main.c"}; !slices.Equal(freshness.ChangedSources, want) {
		t.Fatalf("changedSources = %v, want %v", freshness.ChangedSources, want)
	}
	if freshness.Truncated == nil || *freshness.Truncated {
		t.Fatalf("freshness truncated after edit = %+v, want false", freshness.Truncated)
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("repeat inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("consecutive inspect runs after a source edit are not byte-identical")
	}
}

func TestInspectFreshnessConfigMismatch(t *testing.T) {
	project := writeInspectFreshnessProject(t)
	writeInspectConfig(t, project)
	writeInspectBuildReport(t, project, "2020-01-01T00:00:00Z", "not-the-active-digest")
	source := filepath.Join(project, "src", "main.c")
	setInspectSourceTime(t, []string{source}, time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	if data.BuildReportState == nil || data.BuildReportState.ConfigMatches {
		t.Fatalf("buildReportState configMatches = %+v, want false", data.BuildReportState)
	}
	freshness := data.Freshness
	if freshness == nil || !freshness.Known {
		t.Fatalf("freshness with mismatched config = %+v", freshness)
	}
	if freshness.ConfigMatches == nil || *freshness.ConfigMatches {
		t.Fatalf("freshness configMatches = %+v, want false", freshness.ConfigMatches)
	}
	if freshness.SourceChanged == nil || !*freshness.SourceChanged {
		t.Fatalf("freshness sourceChanged = %+v, want true despite config mismatch", freshness.SourceChanged)
	}
	if want := []string{"proj:src/main.c"}; !slices.Equal(freshness.ChangedSources, want) {
		t.Fatalf("changedSources = %v, want %v", freshness.ChangedSources, want)
	}
}

func TestInspectFreshnessTruncatesChangedSources(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
		ctx.Target("app_again").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})
}
`)
	paths := make([]string, 0, 55)
	for i := 0; i < 55; i++ {
		path := filepath.Join(project, "src", fmt.Sprintf("f%02d.c", i))
		writeExtensionFile(t, path, fmt.Sprintf("int f%d(void) { return %d; }\n", i, i))
		paths = append(paths, path)
	}
	setInspectSourceTime(t, paths, time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	digest := writeInspectConfig(t, project)
	writeInspectBuildReport(t, project, "2020-01-01T00:00:00Z", digest)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	freshness := data.Freshness
	if freshness == nil || !freshness.Known {
		t.Fatalf("freshness = %+v", freshness)
	}
	if freshness.ConfigMatches == nil || !*freshness.ConfigMatches {
		t.Fatalf("freshness configMatches = %+v, want true", freshness.ConfigMatches)
	}
	if freshness.SourceChanged == nil || !*freshness.SourceChanged {
		t.Fatalf("freshness sourceChanged = %+v, want true", freshness.SourceChanged)
	}
	if freshness.Truncated == nil || !*freshness.Truncated {
		t.Fatalf("freshness truncated = %+v, want true", freshness.Truncated)
	}
	if len(freshness.ChangedSources) != inspectFreshnessChangedSourcesLimit {
		t.Fatalf("changedSources length = %d, want %d", len(freshness.ChangedSources), inspectFreshnessChangedSourcesLimit)
	}
	if !slices.IsSorted(freshness.ChangedSources) {
		t.Fatalf("changedSources is not sorted: %v", freshness.ChangedSources)
	}
	seen := make(map[string]bool, len(freshness.ChangedSources))
	for _, source := range freshness.ChangedSources {
		if seen[source] {
			t.Fatalf("changedSources has duplicate %q: %v", source, freshness.ChangedSources)
		}
		seen[source] = true
	}
	if first, last := freshness.ChangedSources[0], freshness.ChangedSources[len(freshness.ChangedSources)-1]; first != "proj:src/f00.c" || last != "proj:src/f49.c" {
		t.Fatalf("changedSources bounds = %q..%q", first, last)
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("repeat inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("freshness output is not deterministic")
	}
}

func TestInspectUnreadableBuildReport(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	reportPath := filepath.Join(project, "build", "vmake-build.json")
	for _, corrupt := range []string{"{this is not a build report\n", "null", "[]", ""} {
		writeExtensionFile(t, reportPath, corrupt)
		stdout, stderr, code := runInspectCommand(t, project, "inspect")
		if code != 0 {
			t.Fatalf("inspect exit = %d, stderr = %q (report %q)", code, stderr, corrupt)
		}
		envelope := decodeInspectEnvelope(t, stdout)
		if envelope.Status != "ok" || envelope.Data == nil {
			t.Fatalf("envelope = %+v (report %q)", envelope, corrupt)
		}
		data := envelope.Data
		if !data.BuildReportPresent || data.BuildReportState != nil {
			t.Fatalf("corrupt report %q: present=%v state=%+v", corrupt, data.BuildReportPresent, data.BuildReportState)
		}
		if data.CompileDatabasePresent {
			t.Fatal("compileDatabasePresent = true without a compile database")
		}
		if data.Freshness == nil || data.Freshness.Known {
			t.Fatalf("corrupt report freshness = %+v, want known false (report %q)", data.Freshness, corrupt)
		}
		if data.Freshness.ReportStatus != "" || data.Freshness.ReportFinished != "" ||
			data.Freshness.ConfigMatches != nil || data.Freshness.SourceChanged != nil ||
			data.Freshness.ChangedSources != nil || data.Freshness.Truncated != nil {
			t.Fatalf("corrupt report freshness exposed fields: %+v (report %q)", data.Freshness, corrupt)
		}
		if len(envelope.Diagnostics) != 1 {
			t.Fatalf("diagnostics = %+v, want one warning (report %q)", envelope.Diagnostics, corrupt)
		}
		diagnostic := envelope.Diagnostics[0]
		if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_REPORT_UNREADABLE" || !strings.Contains(diagnostic.Message, reportPath) {
			t.Fatalf("diagnostic = %+v (report %q)", diagnostic, corrupt)
		}
		if stderr != "Warning: "+diagnostic.Message+"\n" {
			t.Fatalf("stderr = %q, want one warning line (report %q)", stderr, corrupt)
		}
		after, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != corrupt {
			t.Fatalf("inspect rewrote the corrupt build report: %q", after)
		}
	}
}

func writeInspectStdoutProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import (
	"fmt"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		fmt.Println("INSPECT_ONCONFIG_STDOUT")
		ctx.Option("gate").SetType(api.OptionBool).SetDefault(true)
	})
	p.OnRequire(func(ctx *api.RequireContext) {
		fmt.Println("INSPECT_ONREQUIRE_STDOUT")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		fmt.Println("INSPECT_ONBUILD_STDOUT")
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	return project
}

func TestInspectInstallPlan(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnInstall(func(ctx *api.InstallContext) {
		ctx.AddInstalls("oninstall.conf", "etc/oninstall.conf")
		panic("OnInstall must not run during inspect")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.AddInstalls("config/app.conf", "etc/app.conf")
		ctx.AddInstalls("docs/readme.txt", "share/doc/readme.txt")
		ctx.Target("app").SetKind(api.TargetBinary).SetInstallDir("sbin")
		ctx.Target("lib").SetKind(api.TargetStatic)
		ctx.Target("void").SetKind(api.TargetVoid)
		ctx.Target("obj").SetKind(api.TargetObject)
		ctx.Target("off").SetKind(api.TargetBinary).SetInstall(false)
		ctx.Target("tests").SetKind(api.TargetBinary).SetTest(true)
		ctx.Target("fw").SetKind(api.TargetBinary).
			AddPostLinkOutputs("{output}.hex", "{output}.bin")
	})
}
`)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("successful inspect wrote stderr: %q", stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	data := envelope.Data
	if want := filepath.Join(project, "install"); data.InstallPrefix != want {
		t.Errorf("installPrefix = %q, want %q", data.InstallPrefix, want)
	}
	if want := filepath.Join(project, "install", "manifest.json"); data.InstallManifest != want {
		t.Errorf("installManifest = %q, want %q", data.InstallManifest, want)
	}
	proj := inspectPackageNamed(t, data, "proj")
	wantInstalls := []inspectInstall{
		{Source: "config/app.conf", Dest: "etc/app.conf"},
		{Source: "docs/readme.txt", Dest: "share/doc/readme.txt"},
	}
	for _, test := range []struct {
		target      string
		installPath string
	}{
		{"app", "sbin/app"},
		{"lib", "lib/liblib.a"},
		{"void", ""},
		{"obj", ""},
		{"off", ""},
		{"tests", ""},
		{"fw", "bin/fw"},
	} {
		target := inspectTargetNamed(t, proj, test.target)
		if target.InstallPath != test.installPath {
			t.Errorf("%s installPath = %q, want %q", test.target, target.InstallPath, test.installPath)
		}
		if !slices.Equal(target.Installs, wantInstalls) {
			t.Errorf("%s installs = %+v, want the package-level entries", test.target, target.Installs)
		}
	}
	fw := inspectTargetNamed(t, proj, "fw")
	if !slices.Equal(fw.PostLinkOutputs, []string{"{output}.hex", "{output}.bin"}) {
		t.Errorf("fw postLinkOutputs = %v", fw.PostLinkOutputs)
	}
	if strings.Contains(stdout, "oninstall.conf") {
		t.Fatalf("inspect executed OnInstall:\n%s", stdout)
	}
	for _, dir := range []string{"build", "install"} {
		if _, err := os.Stat(filepath.Join(project, dir)); !os.IsNotExist(err) {
			t.Fatalf("inspect created %s: %v", dir, err)
		}
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("consecutive inspect runs are not byte-identical")
	}
}

func TestInspectSuppressesBuildscriptStdout(t *testing.T) {
	project := writeInspectStdoutProject(t)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "INSPECT_") {
		t.Fatalf("buildscript stdout leaked into the JSON document:\n%s", stdout)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Data.Roots) != 1 || envelope.Data.Roots[0] != "proj" {
		t.Fatalf("roots = %v", envelope.Data.Roots)
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect", "--set", "proj/gate=maybe")
	if code != 1 {
		t.Fatalf("invalid --set exit = %d, want 1 (stderr %q)", code, stderr)
	}
	if strings.Contains(stdout, "INSPECT_") {
		t.Fatalf("buildscript stdout leaked into the error envelope:\n%s", stdout)
	}
	envelope = decodeInspectEnvelope(t, stdout)
	if envelope.Status != "error" || envelope.Data != nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_OPTION" {
		t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
	}
	if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") {
		t.Fatalf("stderr = %q, want one human error line", stderr)
	}

	t.Run("configure_failure", func(t *testing.T) {
		broken := filepath.Join(t.TempDir(), "broken")
		writeExtensionFile(t, filepath.Join(broken, "build.go"), `package main

import (
	"fmt"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		fmt.Println("INSPECT_BROKEN_STDOUT")
		panic("configure failure")
	})
}
`)
		stdout, stderr, code := runInspectCommand(t, broken, "inspect")
		if code != 1 {
			t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
		}
		if strings.Contains(stdout, "INSPECT_") {
			t.Fatalf("buildscript stdout leaked into the error envelope:\n%s", stdout)
		}
		envelope := decodeInspectEnvelope(t, stdout)
		if envelope.Status != "error" || envelope.Data != nil {
			t.Fatalf("envelope = %+v", envelope)
		}
		if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_INSPECT" {
			t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
		}
		if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") {
			t.Fatalf("stderr = %q, want one human error line", stderr)
		}
	})
}

func TestInspectSetOverridesAreHypothetical(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())
	configPath := filepath.Join(project, ".vmake", "config.json")

	stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/with_dep=false")
	if code != 0 || stderr != "" {
		t.Fatalf("override inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("override envelope = %+v", envelope)
	}
	data := envelope.Data
	if data.Overrides["proj/with_dep"] != false {
		t.Errorf("overrides = %v", data.Overrides)
	}
	proj := inspectPackageNamed(t, data, "proj")
	if proj.Options["with_dep"] != false {
		t.Errorf("effective with_dep = %v", proj.Options["with_dep"])
	}
	dep := inspectPackageNamed(t, data, "dep")
	if dep.Needed || dep.Targets != nil {
		t.Errorf("dep should not be needed: %+v", dep)
	}
	if len(data.Graph) != 0 {
		t.Errorf("graph = %v, want empty", data.Graph)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("%s was written by inspect --set: %v", configPath, err)
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect", "--set", "dep/dep_mode=slow")
	if code != 0 || stderr != "" {
		t.Fatalf("choice override exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data.Overrides["dep/dep_mode"] != "slow" {
		t.Errorf("overrides = %v", data.Overrides)
	}
	if got := inspectPackageNamed(t, data, "dep").Options["dep_mode"]; got != "slow" {
		t.Errorf("effective dep_mode = %v", got)
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect", "--set", "mode=release")
	if code != 0 || stderr != "" {
		t.Fatalf("global override exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data.Mode != "release" || data.Global["mode"] != "release" || data.Overrides["mode"] != "release" {
		t.Errorf("global override not applied: mode=%q global=%v overrides=%v", data.Mode, data.Global, data.Overrides)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("%s was written by inspect --set: %v", configPath, err)
	}
}

func TestInspectSetTypedCoercion(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())
	configPath := filepath.Join(project, ".vmake", "config.json")

	for _, raw := range []string{"42", "true", "on", "false", ""} {
		t.Run("string_"+raw, func(t *testing.T) {
			stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/label="+raw)
			if code != 0 || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			data := decodeInspectEnvelope(t, stdout).Data
			if data.Overrides["proj/label"] != raw {
				t.Errorf("overrides = %#v, want string %q", data.Overrides, raw)
			}
			if got := inspectPackageNamed(t, data, "proj").Options["label"]; got != raw {
				t.Errorf("effective label = %#v, want string %q", got, raw)
			}
		})
	}

	for _, test := range []struct {
		raw  string
		want bool
	}{
		{"true", true}, {"false", false},
		{"on", true}, {"off", false},
		{"1", true}, {"0", false},
	} {
		t.Run("bool_"+test.raw, func(t *testing.T) {
			stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/with_dep="+test.raw)
			if code != 0 || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			data := decodeInspectEnvelope(t, stdout).Data
			if data.Overrides["proj/with_dep"] != test.want {
				t.Errorf("overrides = %#v, want bool %v", data.Overrides, test.want)
			}
			if got := inspectPackageNamed(t, data, "proj").Options["with_dep"]; got != test.want {
				t.Errorf("effective with_dep = %#v, want bool %v", got, test.want)
			}
		})
	}

	stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/retries=42")
	if code != 0 || stderr != "" {
		t.Fatalf("int override exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	if data.Overrides["proj/retries"] != float64(42) {
		t.Errorf("overrides = %#v, want number 42", data.Overrides)
	}
	if got := inspectPackageNamed(t, data, "proj").Options["retries"]; got != float64(42) {
		t.Errorf("effective retries = %#v, want number 42", got)
	}

	first, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/label=42")
	if code != 0 || stderr != "" {
		t.Fatalf("first typed run exit = %d, stderr = %q", code, stderr)
	}
	again, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/label=42")
	if code != 0 || stderr != "" {
		t.Fatalf("second typed run exit = %d, stderr = %q", code, stderr)
	}
	if again != first {
		t.Fatal("consecutive inspect --set runs are not byte-identical")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("%s was written by inspect --set: %v", configPath, err)
	}
	for _, dir := range []string{"build", "install"} {
		if _, err := os.Stat(filepath.Join(project, dir)); !os.IsNotExist(err) {
			t.Fatalf("inspect --set created %s: %v", dir, err)
		}
	}
}

func TestCoerceInspectOverridesLeavesUndeclaredOptionsRaw(t *testing.T) {
	overrides, err := coerceInspectOverrides([]inspectOverride{
		{pkgName: "proj", optName: "lazy", raw: "on"},
		{pkgName: "proj", optName: "label", raw: "42"},
	}, &inspectOptionDeclarations{
		packageOptions: map[string]map[string]*api.Option{
			"proj": {"label": (&api.Option{}).SetType(api.OptionString)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if overrides[0].value != "on" {
		t.Errorf("undeclared option value = %#v, want raw string \"on\"", overrides[0].value)
	}
	if overrides[1].value != "42" {
		t.Errorf("declared string option value = %#v, want \"42\"", overrides[1].value)
	}

	overrides, err = coerceInspectOverrides([]inspectOverride{{pkgName: "proj", optName: "lazy", raw: "on"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if overrides[0].value != "on" {
		t.Errorf("nil declarations value = %#v, want raw string \"on\"", overrides[0].value)
	}
}

func TestInspectSetErrors(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())
	configPath := filepath.Join(project, ".vmake", "config.json")
	for _, test := range []struct {
		name string
		args []string
	}{
		{"syntax", []string{"--set", "proj/with_dep"}},
		{"unknown_package", []string{"--set", "nope/x=1"}},
		{"unknown_option", []string{"--set", "proj/nope=1"}},
		{"unknown_global", []string{"--set", "nope=1"}},
		{"wrong_type", []string{"--set", "proj/with_dep=maybe"}},
		{"wrong_type_string", []string{"--set", "proj/retries=quick"}},
		{"invalid_choice", []string{"--set", "dep/dep_mode=warp"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, code := runInspectCommand(t, project, append([]string{"inspect"}, test.args...)...)
			if code != 1 {
				t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
			}
			envelope := decodeInspectEnvelope(t, stdout)
			if envelope.Status != "error" || envelope.Data != nil {
				t.Fatalf("envelope = %+v", envelope)
			}
			if len(envelope.Diagnostics) != 1 {
				t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
			}
			diagnostic := envelope.Diagnostics[0]
			if diagnostic.Severity != "error" || diagnostic.Code != "VMAKE_E_OPTION" || diagnostic.Message == "" {
				t.Fatalf("diagnostic = %+v", diagnostic)
			}
			if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") {
				t.Fatalf("stderr = %q, want one human error line", stderr)
			}
			if _, err := os.Stat(configPath); !os.IsNotExist(err) {
				t.Fatalf("%s was written by an invalid --set: %v", configPath, err)
			}
		})
	}
}

func TestInspectSetInvalidBoolValue(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("gate").
			SetType(api.OptionBool).
			SetDefault(true).
			SetOnApply(func(ctx *api.ConfigContext, val any) {
				if !ctx.Bool("gate") {
					return
				}
			})
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetVoid)
	})
}
`)
	stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/gate=maybe")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "error" || envelope.Data != nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_OPTION" || envelope.Diagnostics[0].Severity != "error" {
		t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
	}
	if !strings.Contains(envelope.Diagnostics[0].Message, "invalid bool value") {
		t.Fatalf("diagnostic = %+v, want an invalid bool value message", envelope.Diagnostics[0])
	}
	if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") || strings.Contains(stderr, "panic:") {
		t.Fatalf("stderr = %q, want one human error line", stderr)
	}
}

func TestInspectScriptErrorEnvelope(t *testing.T) {
	for _, test := range []struct {
		name   string
		script string
	}{
		{"load", "package main\n\nfunc Main(p *api.Package) {\n"},
		{"configure", `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) { panic("configure failure") })
}
`},
	} {
		t.Run(test.name, func(t *testing.T) {
			project := filepath.Join(t.TempDir(), "broken")
			writeExtensionFile(t, filepath.Join(project, "build.go"), test.script)
			stdout, stderr, code := runInspectCommand(t, project, "inspect")
			if code != 1 {
				t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
			}
			envelope := decodeInspectEnvelope(t, stdout)
			if envelope.Status != "error" || envelope.Data != nil {
				t.Fatalf("envelope = %+v", envelope)
			}
			if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_INSPECT" || envelope.Diagnostics[0].Severity != "error" {
				t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
			}
			lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
			if len(lines) == 0 || strings.Count(stderr, "Error: ") != 1 || !strings.HasPrefix(lines[len(lines)-1], "Error: ") {
				t.Fatalf("stderr = %q, want one human error line", stderr)
			}
		})
	}
}

func TestInspectTargetDeclarationWarning(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("broken") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetVoid).AddDeps("broken:*")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "broken", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) { panic("declaration failure") })
}
`)
	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
	}
	diagnostic := envelope.Diagnostics[0]
	if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_TARGET_DECLARE" ||
		!strings.Contains(diagnostic.Message, "broken") || diagnostic.Fix == "" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	broken := inspectPackageNamed(t, envelope.Data, "broken")
	if !broken.Needed || broken.Targets != nil {
		t.Fatalf("broken package = %+v", broken)
	}
	if !strings.Contains(stderr, "Warning: cannot declare targets for broken") {
		t.Fatalf("stderr = %q, want one warning line", stderr)
	}
}

func TestInspectSourceMissingWarning(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary)
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "lib", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://example.invalid/lib.git")
		p.AddVersion("1.0.0", "v1.0.0")
	})
}
`)
	libDir := filepath.Join(project, "lib")
	checkWarning := func(t *testing.T) {
		t.Helper()
		stdout, stderr, code := runInspectCommand(t, project, "inspect")
		if code != 0 {
			t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
		}
		envelope := decodeInspectEnvelope(t, stdout)
		if envelope.Status != "ok" || envelope.Data == nil {
			t.Fatalf("envelope = %+v", envelope)
		}
		if len(envelope.Diagnostics) != 1 {
			t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
		}
		diagnostic := envelope.Diagnostics[0]
		if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_SOURCE_MISSING" || diagnostic.Package != "lib" {
			t.Fatalf("diagnostic = %+v", diagnostic)
		}
		for _, want := range []string{"package lib", "https://example.invalid/lib.git", "version 1.0.0"} {
			if !strings.Contains(diagnostic.Message, want) {
				t.Errorf("diagnostic message %q missing %q", diagnostic.Message, want)
			}
		}
		if stderr != "Warning: "+diagnostic.Message+"\n" {
			t.Fatalf("stderr = %q, want one warning line", stderr)
		}
		lib := inspectPackageNamed(t, envelope.Data, "lib")
		if !lib.Needed || lib.SourcePresent {
			t.Fatalf("lib package = %+v", lib)
		}
		if proj := inspectPackageNamed(t, envelope.Data, "proj"); !proj.SourcePresent {
			t.Fatalf("plain local package sourcePresent = false")
		}
	}

	checkWarning(t)
	if err := os.MkdirAll(filepath.Join(libDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	checkWarning(t)

	writeExtensionFile(t, filepath.Join(libDir, "src", "lib.c"), "int lib(void) { return 1; }\n")
	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("materialized inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if len(envelope.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", envelope.Diagnostics)
	}
	if lib := inspectPackageNamed(t, envelope.Data, "lib"); !lib.SourcePresent {
		t.Fatalf("populated lib source reported as absent")
	}
	if _, err := os.Stat(filepath.Join(project, ".vmake_deps")); !os.IsNotExist(err) {
		t.Fatalf("inspect materialized sources: %v", err)
	}
}

func TestInspectSourceMissingWarningRemoteRegistry(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	state := t.TempDir()
	repoDir := filepath.Join(state, "repos", "official")
	writeExtensionFile(t, filepath.Join(repoDir, "packages", "d", "demo", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://example.invalid/demo.git")
		p.AddVersion("1.0.0", "v1.0.0")
	})
}
`)
	runGitLine(t, repoDir, "init", "-q")
	runGitLine(t, repoDir, "config", "user.email", "test@example.com")
	runGitLine(t, repoDir, "config", "user.name", "test")
	runGitLine(t, repoDir, "add", "-A")
	runGitLine(t, repoDir, "commit", "-q", "-m", "registry")

	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("official/demo") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddDeps("official/demo:*")
	})
}
`)
	stdout, stderr, code := runInspectCommandWithState(t, project, state, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
	}
	diagnostic := envelope.Diagnostics[0]
	if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_SOURCE_MISSING" || diagnostic.Package != "official/demo" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	for _, want := range []string{"package official/demo", "https://example.invalid/demo.git", "version 1.0.0"} {
		if !strings.Contains(diagnostic.Message, want) {
			t.Errorf("diagnostic message %q missing %q", diagnostic.Message, want)
		}
	}
	if stderr != "Warning: "+diagnostic.Message+"\n" {
		t.Fatalf("stderr = %q, want one warning line", stderr)
	}
	demo := inspectPackageNamed(t, envelope.Data, "official/demo")
	if !demo.Needed || demo.SourcePresent {
		t.Fatalf("remote package = %+v", demo)
	}
	if _, err := os.Stat(filepath.Join(project, ".vmake_deps")); !os.IsNotExist(err) {
		t.Fatalf("inspect materialized the remote source tree: %v", err)
	}
}

func TestInspectExpectedSourceDirectories(t *testing.T) {
	deps := t.TempDir()
	res := resolver.NewResolver(nil, deps)
	ctx := &RuntimeContext{Paths: &pipeline.Paths{DepsDir: deps}, Resolver: res}
	remoteNode := func(name, dir string) *resolver.PackageNode {
		return resolver.NewPackageNode(name, buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, api.SourceRemote), api.NewPackage())
	}

	registry := remoteNode("official/demo", filepath.Join(deps, "wrapper"))
	registryDir := filepath.Join(deps, "official", "demo", "src")
	if got := inspectExpectedSourceDir(ctx, "official/demo", registry); got != registryDir {
		t.Fatalf("registry source dir = %q, want %q", got, registryDir)
	}
	if inspectSourcePresent(ctx, "official/demo", registry) {
		t.Fatal("absent registry tree reported present")
	}
	if err := os.MkdirAll(registryDir, 0755); err != nil {
		t.Fatal(err)
	}
	if inspectSourcePresent(ctx, "official/demo", registry) {
		t.Fatal("empty registry tree reported present")
	}
	writeExtensionFile(t, filepath.Join(registryDir, "payload.c"), "int demo(void) { return 0; }\n")
	if !inspectSourcePresent(ctx, "official/demo", registry) {
		t.Fatal("populated registry tree reported absent")
	}

	res.SubParents()["native/root/member"] = "native/root"
	member := remoteNode("native/root/member", filepath.Join(deps, "native", "root", "src", "member"))
	memberDir := filepath.Join(deps, "native", "root", "src", "member")
	if got := inspectExpectedSourceDir(ctx, "native/root/member", member); got != memberDir {
		t.Fatalf("shared native member dir = %q, want %q", got, memberDir)
	}
	writeExtensionFile(t, filepath.Join(memberDir, "build.go"), "package main\n")
	if !inspectSourcePresent(ctx, "native/root/member", member) {
		t.Fatal("shared native member tree reported absent")
	}
	member.Pkg.SetGit("https://example.invalid/member.git")
	if got, want := inspectExpectedSourceDir(ctx, "native/root/member", member), filepath.Join(memberDir, "src"); got != want {
		t.Fatalf("own-git native member dir = %q, want %q", got, want)
	}
	if inspectSourcePresent(ctx, "native/root/member", member) {
		t.Fatal("own-git native member tree reported present before materialization")
	}

	localDir := filepath.Join(t.TempDir(), "lib")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatal(err)
	}
	local := resolver.NewPackageNode("lib", buildscript.NewSource("lib", filepath.Join(localDir, "build.go"), localDir, api.SourceLocal), api.NewPackage())
	local.Pkg.SetGit("https://example.invalid/lib.git")
	if got, want := inspectExpectedSourceDir(ctx, "lib", local), filepath.Join(localDir, "src"); got != want {
		t.Fatalf("local SetGit source dir = %q, want %q", got, want)
	}
	if inspectSourcePresent(ctx, "lib", local) {
		t.Fatal("absent local SetGit tree reported present")
	}

	plainDir := filepath.Join(t.TempDir(), "plain")
	writeExtensionFile(t, filepath.Join(plainDir, "build.go"), "package main\n")
	plain := resolver.NewPackageNode("plain", buildscript.NewSource("plain", filepath.Join(plainDir, "build.go"), plainDir, api.SourceLocal), api.NewPackage())
	if got := inspectExpectedSourceDir(ctx, "plain", plain); got != plainDir {
		t.Fatalf("plain local source dir = %q, want %q", got, plainDir)
	}
	if !inspectSourcePresent(ctx, "plain", plain) {
		t.Fatal("plain local package dir reported absent")
	}
}

func checkInspectWarnings(t *testing.T, envelope *inspectEnvelope, stderr string) {
	t.Helper()
	if len(envelope.Diagnostics) == 0 {
		if stderr != "" {
			t.Fatalf("inspect wrote stderr without diagnostics: %q", stderr)
		}
		return
	}
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	if len(lines) != len(envelope.Diagnostics) {
		t.Fatalf("stderr = %q, want one warning line per diagnostic", stderr)
	}
	for i, diagnostic := range envelope.Diagnostics {
		if diagnostic.Severity != "warning" {
			t.Fatalf("diagnostic = %+v, want warning", diagnostic)
		}
		if want := "Warning: " + diagnostic.Message; lines[i] != want {
			t.Fatalf("stderr line %d = %q, want %q", i, lines[i], want)
		}
	}
}

func TestInspectFixtures(t *testing.T) {
	fixtures := []string{
		filepath.Join("..", "..", "test_data", "01_simple_c"),
		filepath.Join("..", "..", "test_data", "03_multi_target"),
		filepath.Join("..", "..", "test_data", "04_multi_module"),
	}
	if runtime.GOOS == "linux" {
		fixtures = append(fixtures, filepath.Join("..", "..", "test_linux", "17_firmware"))
	}
	for _, fixture := range fixtures {
		root, err := filepath.Abs(fixture)
		if err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			t.Skipf("fixture %s unavailable", root)
		}
		t.Run(filepath.Base(root), func(t *testing.T) {
			before, err := os.ReadFile(filepath.Join(root, ".vmake", "config.json"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			stdout, stderr, code := runInspectCommand(t, root, "inspect")
			if code != 0 {
				t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
			}
			envelope := decodeInspectEnvelope(t, stdout)
			if envelope.SchemaVersion != 1 || envelope.Command != "inspect" || envelope.Status != "ok" || envelope.Data == nil {
				t.Fatalf("envelope = %+v", envelope)
			}
			checkInspectWarnings(t, envelope, stderr)
			data := envelope.Data
			if data.Root != root || data.Mode == "" || data.Toolchain == "" || data.CompileDatabase == "" || data.BuildReport == "" {
				t.Fatalf("data header = %+v", data)
			}
			if len(data.Packages) == 0 || len(data.Roots) == 0 {
				t.Fatalf("empty model: roots=%v packages=%d", data.Roots, len(data.Packages))
			}
			for _, pkg := range data.Packages {
				if pkg.Name == "" || pkg.Kind == "" || pkg.SourceDir == "" || pkg.Deps == nil {
					t.Fatalf("incomplete package entry: %+v", pkg)
				}
				for _, target := range pkg.Targets {
					if target.Name == "" || target.Kind == "" {
						t.Fatalf("incomplete target entry: %+v", target)
					}
				}
			}
			if data.ToolchainInfo == nil || data.ToolchainInfo.Name == "" || data.ToolchainInfo.CC == "" {
				t.Fatalf("toolchainInfo = %+v", data.ToolchainInfo)
			}
			if data.Global == nil || data.Graph == nil {
				t.Fatalf("global = %v, graph = %v", data.Global, data.Graph)
			}
			again, stderr, code := runInspectCommand(t, root, "inspect")
			if code != 0 {
				t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
			}
			if again != stdout {
				t.Fatal("consecutive inspect runs are not byte-identical")
			}
			checkInspectWarnings(t, decodeInspectEnvelope(t, again), stderr)
			after, err := os.ReadFile(filepath.Join(root, ".vmake", "config.json"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("inspect modified .vmake/config.json")
			}
		})
	}
}

func TestInspectCommandRegistration(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"inspect"})
	if err != nil {
		t.Fatalf("inspect not registered: %v", err)
	}
	if cmd.Name() != "inspect" {
		t.Fatalf("command = %q", cmd.Name())
	}
	if cmd.Flags().Lookup("set") == nil {
		t.Fatal("inspect has no --set flag")
	}
	ref := generateCLIRef(RootCmd)
	if !strings.Contains(ref, "vmake inspect") || !strings.Contains(ref, "--set <value>") {
		t.Fatalf("CLI reference missing inspect:\n%s", ref)
	}
}

func TestInspectSkipsTargetDeclarationForMissingSource(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddDeps("lib:lib")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "lib", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://example.invalid/lib.git")
		p.AddVersion("1.0.0", "v1.0.0")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		panic("declaration failure")
	})
}
`)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_W_SOURCE_MISSING" {
		t.Fatalf("diagnostics = %+v, want only the source-missing warning", envelope.Diagnostics)
	}
	lib := inspectPackageNamed(t, envelope.Data, "lib")
	if !lib.Needed || lib.SourcePresent || lib.Targets != nil {
		t.Fatalf("lib package = %+v, want needed missing source without targets", lib)
	}
	if strings.Contains(stderr, "panic") || strings.Contains(stderr, "VMAKE_W_TARGET_DECLARE") {
		t.Fatalf("stderr = %q, want only the source warning", stderr)
	}
	if stderr != "Warning: "+envelope.Diagnostics[0].Message+"\n" {
		t.Fatalf("stderr = %q, want one warning line", stderr)
	}
	proj := inspectPackageNamed(t, envelope.Data, "proj")
	if targets := proj.Targets; len(targets) != 1 || targets[0].Name != "app" {
		t.Fatalf("proj targets = %+v, want app declared for its present source", targets)
	}
}

func TestInspectReportsBuildAndTestStateSeparately(t *testing.T) {
	project := writeTestCommandProject(t, passingTestSource)
	if stdout, stderr, code := runBuildCommand(t, project, "build"); code != 0 {
		t.Fatalf("build exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	if stdout, stderr, code := runBuildCommand(t, project, "test"); code != 0 {
		t.Fatalf("test exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	buildStateBefore := loadBuildReport(t, project)
	testStateBefore := loadTestReport(t, project)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	if data == nil {
		t.Fatal("inspect returned no data")
	}
	if !data.BuildReportPresent || data.BuildReportState == nil {
		t.Fatalf("build report state missing: present=%v state=%+v", data.BuildReportPresent, data.BuildReportState)
	}
	if data.BuildReportState.Command != reportCommandBuild || data.BuildReportState.BuildID != buildStateBefore.BuildID ||
		data.BuildReportState.Status != "succeeded" || !data.BuildReportState.ConfigMatches {
		t.Fatalf("buildReportState = %+v, report = %+v", data.BuildReportState, buildStateBefore)
	}
	if data.TestReport != testReportPath(project) || !data.TestReportPresent || data.TestReportState == nil {
		t.Fatalf("test report state missing: path=%q present=%v state=%+v", data.TestReport, data.TestReportPresent, data.TestReportState)
	}
	if data.TestReportState.Command != reportCommandTest || data.TestReportState.BuildID != testStateBefore.BuildID ||
		data.TestReportState.Status != "succeeded" || !data.TestReportState.ConfigMatches {
		t.Fatalf("testReportState = %+v, report = %+v", data.TestReportState, testStateBefore)
	}
	if string(data.TestReportState.Summary) == "" {
		t.Fatal("testReportState lost the summary")
	}

	if stdout, stderr, code := runInspectCommand(t, project, "clean"); code != 0 {
		t.Fatalf("clean exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	stdout, stderr, code = runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect after clean exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data == nil {
		t.Fatal("inspect after clean returned no data")
	}
	if data.BuildReportPresent || data.BuildReportState != nil || data.TestReportPresent || data.TestReportState != nil {
		t.Fatalf("reports survived clean: build=%v/%+v test=%v/%+v",
			data.BuildReportPresent, data.BuildReportState, data.TestReportPresent, data.TestReportState)
	}
}
