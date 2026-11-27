package buildreport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/version"
)

const (
	CommandBuild   = "build"
	CommandRebuild = "rebuild"
	CommandTest    = "test"

	BuildReportFile = "vmake-build.json"
	TestReportFile  = "vmake-test.json"
)

type Artifact struct {
	Package     string `json:"package"`
	Target      string `json:"target"`
	BuildPath   string `json:"buildPath"`
	InstallPath string `json:"installPath"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type Summary struct {
	DurationMs         int64    `json:"durationMs"`
	TargetsTotal       int      `json:"targetsTotal"`
	TargetsBuilt       int      `json:"targetsBuilt"`
	TargetsUpToDate    int      `json:"targetsUpToDate"`
	TargetsFailed      int      `json:"targetsFailed"`
	TestsFound         int      `json:"testsFound"`
	TestsPassed        int      `json:"testsPassed"`
	TestsFailed        int      `json:"testsFailed"`
	FilesCompiled      int      `json:"filesCompiled"`
	FilesLinked        int      `json:"filesLinked"`
	ActivitiesUpToDate int      `json:"actionsUpToDate"`
	ArtifactsInstalled int      `json:"artifactsInstalled"`
	ChangedFiles       []string `json:"changedFiles"`
	Callbacks          int      `json:"callbacks"`
	ExternalStages     int      `json:"externalStages"`
	NoOp               bool     `json:"noOp"`
}

type TestResult struct {
	Name       string `json:"name"`
	Package    string `json:"package"`
	Target     string `json:"target"`
	Passed     bool   `json:"passed"`
	NotRun     bool   `json:"notRun,omitempty"`
	SkipReason string `json:"skipReason,omitempty"`
	ElapsedMs  int64  `json:"elapsedMs"`
	ExitCode   int    `json:"exitCode"`
	Signal     string `json:"signal,omitempty"`
	Output     string `json:"output,omitempty"`
}

type Report struct {
	Completed       bool `json:"-"`
	location        string
	configSelector  string
	SchemaVersion   int                       `json:"schemaVersion"`
	BuildID         string                    `json:"buildId"`
	Root            string                    `json:"root"`
	Status          string                    `json:"status"`
	Command         string                    `json:"command,omitempty"`
	VMakeVersion    string                    `json:"vmakeVersion,omitempty"`
	VMakeCommit     string                    `json:"vmakeCommit,omitempty"`
	Started         string                    `json:"started"`
	Finished        string                    `json:"finished"`
	DurationMs      int64                     `json:"durationMs"`
	Error           string                    `json:"error"`
	Diagnostics     []Diagnostic              `json:"diagnostics"`
	Summary         *Summary                  `json:"summary,omitempty"`
	ConfigFile      string                    `json:"configFile"`
	ConfigSHA256    string                    `json:"configSha256"`
	Global          map[string]any            `json:"global"`
	Packages        map[string]map[string]any `json:"packages"`
	Toolchain       string                    `json:"toolchain"`
	Mode            string                    `json:"mode"`
	CompileDatabase string                    `json:"compileDatabase"`
	Targets         []build.TargetResult      `json:"targets"`
	Tests           []TestResult              `json:"tests"`
	Artifacts       []Artifact                `json:"artifacts"`
}

type SummaryError struct {
	Summary string
	Err     error
}

func (e *SummaryError) Error() string { return e.Summary }

func (e *SummaryError) Unwrap() error { return e.Err }

func ReportPath(root string) string { return filepath.Join(root, "build", BuildReportFile) }

func TestReportPath(root string) string { return filepath.Join(root, "build", TestReportFile) }

func ReportPathFor(root, command string) string {
	if command == CommandTest {
		return TestReportPath(root)
	}
	return ReportPath(root)
}

func (r *Report) ReportPath() string {
	if r.location != "" {
		return r.location
	}
	return ReportPathFor(r.Root, r.Command)
}

func (r *Report) SetLocation(loc string) {
	r.location = loc
}

func (r *Report) SetConfigSelector(selector string) {
	r.configSelector = selector
}

func Begin(root, command, configSelector string) (*Report, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	r := &Report{
		SchemaVersion:  2,
		BuildID:        hex.EncodeToString(id),
		Root:           root,
		Status:         "running",
		Command:        command,
		VMakeVersion:   version.Version,
		VMakeCommit:    version.GitCommit,
		Started:        time.Now().UTC().Format(time.RFC3339Nano),
		Targets:        []build.TargetResult{},
		Tests:          []TestResult{},
		Artifacts:      []Artifact{},
		Diagnostics:    []Diagnostic{},
		Global:         map[string]any{},
		Packages:       map[string]map[string]any{},
		location:       ReportPathFor(root, command),
		configSelector: configSelector,
	}
	return r, jsonio.Save(r.ReportPath(), r)
}

func (r *Report) Finish(result *error) {
	interrupted := recover()
	if interrupted != nil {
		*result = fmt.Errorf("build panicked: %v", interrupted)
	}
	if *result == nil && !r.Completed {
		*result = fmt.Errorf("build interrupted before results were collected")
	}
	r.Finished = time.Now().UTC().Format(time.RFC3339Nano)
	r.DurationMs = ReportDuration(r.Started, r.Finished)
	diagnostics, summary := FailureDetails(*result)
	if *result != nil && r.Command == CommandTest && !errors.Is(*result, context.Canceled) && !hasErrorDiagnostic(diagnostics) {
		diagnostics = append(diagnostics, Diagnostic{Severity: "error", Code: "VMAKE_E_TEST", Message: summary})
	}
	r.Diagnostics, r.Error = diagnostics, summary
	r.Status = "succeeded"
	if *result != nil {
		if errors.Is(*result, context.Canceled) {
			r.Status = "cancelled"
		} else {
			r.Status = "failed"
		}
		r.Artifacts = []Artifact{}
	}
	if r.Summary != nil {
		r.Summary.DurationMs = r.DurationMs
		if r.Status == "failed" {
			r.Summary.NoOp = false
		}
	}
	if r.Tests == nil {
		r.Tests = []TestResult{}
	}
	r.setCompileDatabase()
	*result = errors.Join(*result, jsonio.Save(r.ReportPath(), r))
	if interrupted != nil {
		panic(interrupted)
	}
}

func hasErrorDiagnostic(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}

func (r *Report) setCompileDatabase() {
	path := filepath.Join(r.Root, "build", "compile_commands.json")
	if _, err := os.Stat(path); err == nil {
		r.CompileDatabase = path
	}
}

func ReportDuration(started, finished string) int64 {
	start, err := time.Parse(time.RFC3339Nano, started)
	if err != nil {
		return 0
	}
	end, err := time.Parse(time.RFC3339Nano, finished)
	if err != nil {
		return 0
	}
	duration := end.Sub(start)
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}

func FileDigest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func reportConfigFile(root, configPath string) string {
	if rel, err := filepath.Rel(root, configPath); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(configPath)
}

func (r *Report) Bind(ctx *pipeline.RuntimeContext) error {
	r.ConfigFile, r.ConfigSHA256 = reportConfigFile(r.Root, ctx.ConfigPath), ctx.ConfigDigest
	r.Toolchain = pipeline.ResolveToolchainName(ctx.Config, ctx.ToolchainOverride, ctx.PreferredToolchain)
	r.Mode = pipeline.ResolveMode(ctx.Config, ctx.ModeOverride)
	r.Global = pipeline.ReportGlobalValues(ctx)
	return jsonio.Save(r.ReportPath(), r)
}

func (r *Report) Partial(ctx *pipeline.RuntimeContext, result *pipeline.BuildResult, err error) {
	r.Global, r.Targets = result.GlobalValues, result.Targets
	r.Toolchain, r.Mode = result.TcName, result.Mode
	if r.Global == nil {
		r.Global = pipeline.ReportGlobalValues(ctx)
	}
	r.setCompileDatabase()
	for name, buildContext := range result.BuildCtxs {
		r.Packages[name] = maps.Clone(api.BindCfgVals(&buildContext.ConfigAccessor))
	}
	r.Summary = NewSummary(result, 0)
	r.Summary.TargetsFailed = failedTargetCount(err)
	r.Summary.NoOp = false
}

func failedTargetCount(err error) int {
	diagnostics, _ := FailureDetails(err)
	targets := make(map[string]bool)
	for _, diagnostic := range diagnostics {
		if diagnostic.Target != "" {
			targets[diagnostic.Target] = true
		}
	}
	if len(targets) > 0 {
		return len(targets)
	}
	var targetErr *build.TargetError
	if errors.As(err, &targetErr) {
		return 1
	}
	return 0
}

func (r *Report) SetTestResults(tests []pipeline.TestResult) {
	r.Tests = make([]TestResult, len(tests))
	passed, failed := 0, 0
	for i, test := range tests {
		r.Tests[i] = TestResult{
			Name:       test.FullName,
			Package:    test.PackageName,
			Target:     test.TargetName,
			Passed:     test.Passed,
			NotRun:     test.NotRun,
			SkipReason: test.SkipReason,
			ElapsedMs:  test.Elapsed.Milliseconds(),
			ExitCode:   test.ExitCode,
			Signal:     test.Signal,
			Output:     test.Output,
		}
		if test.NotRun {
			continue
		}
		if test.Passed {
			passed++
		} else {
			failed++
		}
	}
	if r.Summary != nil {
		r.Summary.TestsFound = len(tests)
		r.Summary.TestsPassed = passed
		r.Summary.TestsFailed = failed
	}
}

func (r *Report) Complete(ctx *pipeline.RuntimeContext, result *pipeline.BuildResult) error {
	r.ConfigFile, r.ConfigSHA256 = reportConfigFile(r.Root, ctx.ConfigPath), ctx.ConfigDigest
	_, path, digest, err := config.LoadProjectSnapshotWithOverride(r.Root, r.configSelector)
	if err != nil {
		return err
	}
	if path != ctx.ConfigPath || digest != ctx.ConfigDigest {
		return fmt.Errorf("configuration changed during build")
	}
	r.Global, r.Targets = result.GlobalValues, result.Targets
	r.Toolchain, r.Mode = result.TcName, result.Mode
	r.CompileDatabase = filepath.Join(r.Root, "build", "compile_commands.json")
	if _, err := os.Stat(r.CompileDatabase); os.IsNotExist(err) {
		r.CompileDatabase = ""
	} else if err != nil {
		return err
	}
	for name, buildContext := range result.BuildCtxs {
		r.Packages[name] = maps.Clone(api.BindCfgVals(&buildContext.ConfigAccessor))
	}

	seen := map[string]bool{}
	for _, file := range result.InstallFiles {
		source, err := filepath.Abs(file.Source)
		if err != nil {
			return err
		}
		installed, err := filepath.Abs(file.Destination)
		if err != nil {
			return err
		}
		key := source + "\x00" + installed
		if seen[key] {
			continue
		}
		hash, size, err := FileDigest(source)
		if err != nil {
			return err
		}
		installedHash, _, err := FileDigest(installed)
		if err != nil {
			return err
		}
		if installedHash != hash {
			return fmt.Errorf("installed artifact differs: %s", installed)
		}
		seen[key] = true
		r.Artifacts = append(r.Artifacts, Artifact{file.Package, file.Target, source, installed, size, hash})
	}
	slices.SortFunc(r.Artifacts, func(a, b Artifact) int {
		if c := strings.Compare(a.BuildPath, b.BuildPath); c != 0 {
			return c
		}
		return strings.Compare(a.InstallPath, b.InstallPath)
	})
	r.Summary = NewSummary(result, len(r.Artifacts))
	r.Completed = true
	return nil
}

func NewSummary(result *pipeline.BuildResult, artifacts int) *Summary {
	changedFiles := result.Stats.CompiledFiles
	if changedFiles == nil {
		changedFiles = []string{}
	}
	summary := &Summary{
		TargetsTotal:       len(result.Targets),
		FilesCompiled:      result.Stats.Compiled,
		FilesLinked:        result.Stats.Linked,
		Callbacks:          result.Stats.Callbacks,
		ActivitiesUpToDate: result.Stats.UpToDate,
		ArtifactsInstalled: artifacts,
		ChangedFiles:       changedFiles,
		ExternalStages:     result.ExternalStages(),
	}
	for _, target := range result.Targets {
		switch target.Status {
		case build.TargetStatusBuilt:
			summary.TargetsBuilt++
		case build.TargetStatusUpToDate:
			summary.TargetsUpToDate++
		}
	}
	summary.NoOp = summary.FilesCompiled == 0 && summary.FilesLinked == 0 &&
		len(summary.ChangedFiles) == 0 && summary.ExternalStages == 0
	return summary
}

func PrintSummary(writer io.Writer, report *Report, err error) {
	if report.Command == CommandTest {
		PrintTestSummary(writer, report, err)
		return
	}
	if err != nil {
		message := report.Error
		if message == "" {
			message = FirstErrorLine(err)
		}
		fmt.Fprintf(writer, "Build failed: %s\n", message)
		return
	}
	summary := report.Summary
	if summary == nil {
		fmt.Fprintf(writer, "Build succeeded (buildId=%s)\n", report.BuildID)
		return
	}
	if summary.NoOp {
		fmt.Fprintf(writer, "Build succeeded: no compilation (%d targets, %d up to date, %d callbacks, %d installed) in %s (buildId=%s)\n",
			summary.TargetsTotal, summary.TargetsUpToDate, summary.Callbacks,
			summary.ArtifactsInstalled, FormatDuration(summary.DurationMs), report.BuildID)
		return
	}
	callbacks := ""
	if summary.Callbacks > 0 {
		callbacks = fmt.Sprintf(", %d callbacks", summary.Callbacks)
	}
	external := ""
	if summary.ExternalStages > 0 {
		external = fmt.Sprintf(", %d external stages", summary.ExternalStages)
	}
	fmt.Fprintf(writer, "Build succeeded: %d targets (%d built, %d up to date)%s, %d compiled, %d linked, %d installed%s in %s (buildId=%s)\n",
		summary.TargetsTotal, summary.TargetsBuilt, summary.TargetsUpToDate, callbacks,
		summary.FilesCompiled, summary.FilesLinked, summary.ArtifactsInstalled, external,
		FormatDuration(summary.DurationMs), report.BuildID)
}

func PrintTestSummary(writer io.Writer, report *Report, err error) {
	if err != nil {
		message := report.Error
		if message == "" {
			message = FirstErrorLine(err)
		}
		fmt.Fprintf(writer, "Tests failed: %s\n", message)
		return
	}
	summary := report.Summary
	if summary == nil {
		fmt.Fprintf(writer, "Tests succeeded (buildId=%s)\n", report.BuildID)
		return
	}
	if summary.TestsFound == 0 {
		fmt.Fprintf(writer, "Tests succeeded: no test targets (buildId=%s)\n", report.BuildID)
		return
	}
	fmt.Fprintf(writer, "Tests succeeded: %d/%d passed in %s (buildId=%s)\n",
		summary.TestsPassed, summary.TestsFound, FormatDuration(summary.DurationMs), report.BuildID)
}

func FormatDuration(durationMs int64) string {
	if durationMs < 1000 {
		return fmt.Sprintf("%dms", durationMs)
	}
	return fmt.Sprintf("%.1fs", float64(durationMs)/1000)
}

func InvalidateReports(root string) error {
	return errors.Join(removeReport(ReportPath(root)), removeReport(TestReportPath(root)))
}

func removeReport(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
