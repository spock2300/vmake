package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	checkLibSource  = "int lib_value(void);\nint lib_value(void) { return 7; }\n"
	checkMainSource = "int lib_value(void);\nint main(void) { return lib_value() == 7 ? 0 : 1; }\n"
)

func checkProjectBuildGo(appFlags string) string {
	return `package main

import (
	"fmt"
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		fmt.Println("CHECK SCRIPT NOISE")
		ctx.Target("lib").SetKind(api.TargetStatic).AddFiles("src/lib.c")
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")` + appFlags + `.AddDeps("checkproj:lib")
		ctx.Target("gen").SetKind(api.TargetVoid).SetBuildFunc(func(p *api.Package) error {
			return os.WriteFile("gen-marker.txt", []byte("ran\n"), 0644)
		})
	})
}
`
}

func writeCheckProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "checkproj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), checkProjectBuildGo(""))
	writeExtensionFile(t, filepath.Join(project, "src", "lib.c"), checkLibSource)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), checkMainSource)
	return project
}

type checkTestData struct {
	WorkPending      bool `json:"workPending"`
	PlannedCompiles  int  `json:"plannedCompiles"`
	PlannedLinks     int  `json:"plannedLinks"`
	PlannedCallbacks int  `json:"plannedCallbacks"`
	UpToDate         int  `json:"upToDate"`
	TargetsTotal     int  `json:"targetsTotal"`
}

type checkTestDocument struct {
	SchemaVersion int               `json:"schemaVersion"`
	Command       string            `json:"command"`
	Status        string            `json:"status"`
	Data          *checkTestData    `json:"data"`
	Diagnostics   []buildDiagnostic `json:"diagnostics"`
}

func parseCheckJSON(t *testing.T, stdout string) checkTestDocument {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(stdout))
	var document checkTestDocument
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout contains more than one JSON document (%v)\n%s", err, stdout)
	}
	if document.SchemaVersion != 1 || document.Command != reportCommandCheck {
		t.Fatalf("check envelope = %+v", document)
	}
	if document.Diagnostics == nil {
		t.Fatalf("diagnostics must be an array: %+v", document)
	}
	return document
}

func runCheckJSON(t *testing.T, project string, args ...string) (checkTestDocument, string, string) {
	t.Helper()
	stdout, stderr, code := runBuildCommand(t, project, args...)
	if code != 0 {
		t.Fatalf("%v exit = %d\nstdout:\n%s\nstderr:\n%s", args, code, stdout, stderr)
	}
	document := parseCheckJSON(t, stdout)
	if document.Status != "ok" || document.Data == nil {
		t.Fatalf("check envelope = %+v", document)
	}
	return document, stdout, stderr
}

func assertCheckPlan(t *testing.T, data *checkTestData, workPending bool, compiles, links, callbacks, upToDate, targets int) {
	t.Helper()
	if data.WorkPending != workPending || data.PlannedCompiles != compiles || data.PlannedLinks != links ||
		data.PlannedCallbacks != callbacks || data.UpToDate != upToDate || data.TargetsTotal != targets {
		t.Fatalf("plan = %+v, want workPending=%v compiles=%d links=%d callbacks=%d upToDate=%d targets=%d",
			data, workPending, compiles, links, callbacks, upToDate, targets)
	}
}

func assertNoCheckArtifacts(t *testing.T, project string) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(project, "build"),
		filepath.Join(project, "install"),
		filepath.Join(project, "gen-marker.txt"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("check created %s: %v", path, err)
		}
	}
}

func snapshotCheckDir(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = fmt.Sprintf("%d|%d|%x", info.Size(), info.ModTime().UnixNano(), sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestBuildCheckColdPlanAndJSON(t *testing.T) {
	project := writeCheckProject(t)
	stdout, stderr, code := runBuildCommand(t, project, "build", "--check")
	if code != 0 {
		t.Fatalf("cold check exit = %d, stderr = %q", code, stderr)
	}
	want := "Build check: 2 compiles, 2 links, 1 callback pending (0 actions up to date)"
	if strings.TrimSpace(stdout) != want {
		t.Fatalf("cold check stdout = %q, want %q", stdout, want)
	}
	if !strings.Contains(stderr, "CHECK SCRIPT NOISE") {
		t.Fatalf("buildscript output is missing from stderr:\n%s", stderr)
	}
	assertNoCheckArtifacts(t, project)

	document, jsonStdout, jsonStderr := runCheckJSON(t, project, "build", "--check", "--json")
	assertCheckPlan(t, document.Data, true, 2, 2, 1, 0, 3)
	if len(document.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want []", document.Diagnostics)
	}
	if strings.Contains(jsonStdout, "Build check") || strings.Contains(jsonStdout, "CHECK SCRIPT NOISE") {
		t.Fatalf("JSON stdout contains human text:\n%s", jsonStdout)
	}
	if !strings.Contains(jsonStderr, "CHECK SCRIPT NOISE") {
		t.Fatalf("buildscript output is missing from stderr:\n%s", jsonStderr)
	}
	assertNoCheckArtifacts(t, project)
}

func TestBuildCheckAfterBuildIsUpToDateAndNonMutating(t *testing.T) {
	project := writeCheckProject(t)
	stdout, stderr, code := runBuildCommand(t, project, "build")
	if code != 0 {
		t.Fatalf("build exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	reportPath := filepath.Join(project, "build", buildReportFile)
	compileDB := filepath.Join(project, "build", "compile_commands.json")
	markerPath := filepath.Join(project, "gen-marker.txt")
	reportBefore := mustReadCheckFile(t, reportPath)
	compileDBBefore := mustReadCheckFile(t, compileDB)
	markerInfo, err := os.Stat(markerPath)
	if err != nil {
		t.Fatalf("void callback did not run during the build: %v", err)
	}
	buildBefore := snapshotCheckDir(t, filepath.Join(project, "build"))

	stdout, stderr, code = runBuildCommand(t, project, "build", "--check")
	if code != 0 {
		t.Fatalf("check exit = %d, stderr = %q", code, stderr)
	}
	want := "Build check: up to date (no compile/link work; 1 callback would run)"
	if strings.TrimSpace(stdout) != want {
		t.Fatalf("up-to-date check stdout = %q, want %q", stdout, want)
	}
	if got := mustReadCheckFile(t, reportPath); !bytes.Equal(got, reportBefore) {
		t.Fatal("check rewrote the build report")
	}
	if got := mustReadCheckFile(t, compileDB); !bytes.Equal(got, compileDBBefore) {
		t.Fatal("check rewrote the compilation database")
	}
	if after, err := os.Stat(markerPath); err != nil || !after.ModTime().Equal(markerInfo.ModTime()) {
		t.Fatalf("check executed the void callback: %v", err)
	}
	if after := snapshotCheckDir(t, filepath.Join(project, "build")); !maps.Equal(buildBefore, after) {
		t.Fatalf("check mutated the build directory:\nbefore: %v\nafter:  %v", buildBefore, after)
	}

	document, _, _ := runCheckJSON(t, project, "build", "--check", "--json")
	assertCheckPlan(t, document.Data, false, 0, 0, 1, 4, 3)
	if got := mustReadCheckFile(t, reportPath); !bytes.Equal(got, reportBefore) {
		t.Fatal("JSON check rewrote the build report")
	}
	if got := mustReadCheckFile(t, compileDB); !bytes.Equal(got, compileDBBefore) {
		t.Fatal("JSON check rewrote the compilation database")
	}
	if after := snapshotCheckDir(t, filepath.Join(project, "build")); !maps.Equal(buildBefore, after) {
		t.Fatalf("JSON check mutated the build directory:\nbefore: %v\nafter:  %v", buildBefore, after)
	}
}

func mustReadCheckFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBuildCheckDetectsSourceFlagAndScriptChanges(t *testing.T) {
	project := writeCheckProject(t)
	if _, stderr, code := runBuildCommand(t, project, "build"); code != 0 {
		t.Fatalf("build exit = %d, stderr = %q", code, stderr)
	}
	document, _, _ := runCheckJSON(t, project, "build", "--check", "--json")
	assertCheckPlan(t, document.Data, false, 0, 0, 1, 4, 3)

	mainPath := filepath.Join(project, "src", "main.c")
	writeExtensionFile(t, mainPath, checkMainSource+"/* edited */\n")
	document, _, _ = runCheckJSON(t, project, "build", "--check", "--json")
	assertCheckPlan(t, document.Data, true, 1, 1, 1, 2, 3)

	writeExtensionFile(t, mainPath, checkMainSource)
	document, _, _ = runCheckJSON(t, project, "build", "--check", "--json")
	assertCheckPlan(t, document.Data, false, 0, 0, 1, 4, 3)

	buildGoPath := filepath.Join(project, "build.go")
	writeExtensionFile(t, buildGoPath, checkProjectBuildGo(`.AddCFlags("-DCHECK_PROBE=1")`))
	document, _, _ = runCheckJSON(t, project, "build", "--check", "--json")
	assertCheckPlan(t, document.Data, true, 1, 1, 1, 2, 3)

	writeExtensionFile(t, buildGoPath, checkProjectBuildGo(""))
	writeExtensionFile(t, buildGoPath, checkProjectBuildGo("")+"// comment only\n")
	document, _, _ = runCheckJSON(t, project, "build", "--check", "--json")
	assertCheckPlan(t, document.Data, false, 0, 0, 1, 4, 3)
}

func TestBuildCheckJSONErrorEnvelope(t *testing.T) {
	project := filepath.Join(t.TempDir(), "brokencheck")
	writeExtensionFile(t, filepath.Join(project, "build.go"), "package main\n\nfunc Main( {\n")
	stdout, stderr, code := runBuildCommand(t, project, "build", "--check", "--json")
	if code != 1 {
		t.Fatalf("broken check exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	document := parseCheckJSON(t, stdout)
	if document.Status != "error" || document.Data != nil {
		t.Fatalf("error envelope = %+v", document)
	}
	if len(document.Diagnostics) == 0 {
		t.Fatalf("error envelope has no diagnostics: %+v", document)
	}
	for _, diagnostic := range document.Diagnostics {
		if diagnostic.Severity != "error" || diagnostic.Code == "" || diagnostic.Message == "" {
			t.Fatalf("diagnostic = %+v", diagnostic)
		}
	}
	if _, err := os.Stat(filepath.Join(project, "build", buildReportFile)); !os.IsNotExist(err) {
		t.Fatalf("failed check published a build report: %v", err)
	}

	broken := filepath.Join(t.TempDir(), "brokenhuman")
	writeExtensionFile(t, filepath.Join(broken, "build.go"), "package main\n\nfunc Main( {\n")
	humanStdout, _, humanCode := runBuildCommand(t, broken, "build", "--check")
	if humanCode != 1 {
		t.Fatalf("human broken check exit = %d, want 1", humanCode)
	}
	if !strings.HasPrefix(strings.TrimSpace(humanStdout), "Build check failed: ") {
		t.Fatalf("human failure stdout = %q", humanStdout)
	}
}

func TestBuildCheckRejectsInstallAndTests(t *testing.T) {
	project := writeCheckProject(t)
	for _, test := range []struct {
		flag string
		want string
	}{
		{"--install", "--check cannot be combined with --install"},
		{"--tests", "--check cannot be combined with --tests"},
	} {
		stdout, stderr, code := runBuildCommand(t, project, "build", "--check", test.flag)
		if code != 2 {
			t.Fatalf("build --check %s exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", test.flag, code, stdout, stderr)
		}
		if stdout != "" {
			t.Fatalf("build --check %s stdout = %q, want empty", test.flag, stdout)
		}
		if !strings.Contains(stderr, test.want) {
			t.Fatalf("build --check %s stderr missing %q:\n%s", test.flag, test.want, stderr)
		}
	}
	assertNoCheckArtifacts(t, project)
}

func TestDetectPreReportJSONCheck(t *testing.T) {
	request := detectPreReportJSON([]string{"build", "--check", "--json"})
	if request == nil || request.command != reportCommandCheck || !request.requested {
		t.Fatalf("check request = %+v", request)
	}
	if request := detectPreReportJSON([]string{"build", "--plan", "--json"}); request == nil || request.command != reportCommandCheck {
		t.Fatalf("plan request = %+v", request)
	}
	if request := detectPreReportJSON([]string{"build", "--json"}); request == nil || request.command != reportCommandBuild {
		t.Fatalf("build request = %+v", request)
	}
}

func TestBuildCheckQuietPlanAliasAndOutput(t *testing.T) {
	project := writeCheckProject(t)
	quiet, stderr, code := runBuildCommand(t, project, "-q", "build", "--check")
	if code != 0 || stderr != "" {
		t.Fatalf("quiet check exit = %d, stderr = %q", code, stderr)
	}
	want := "Build check: 2 compiles, 2 links, 1 callback pending (0 actions up to date)"
	if strings.TrimSpace(quiet) != want {
		t.Fatalf("quiet check stdout = %q, want %q", quiet, want)
	}

	alias, _, code := runBuildCommand(t, project, "-q", "build", "--plan")
	if code != 0 || alias != quiet {
		t.Fatalf("--plan output = %q (exit %d), want %q", alias, code, quiet)
	}

	output := filepath.Join(t.TempDir(), "check.json")
	stdout, _, code := runBuildCommand(t, project, "build", "--check", "--output", output)
	if code != 0 {
		t.Fatalf("check --output exit = %d", code)
	}
	if stdout != "" {
		t.Fatalf("check --output stdout = %q, want empty", stdout)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	document := parseCheckJSON(t, string(data))
	if document.Status != "ok" || document.Data == nil {
		t.Fatalf("output envelope = %+v", document)
	}
	assertCheckPlan(t, document.Data, true, 2, 2, 1, 0, 3)
}
