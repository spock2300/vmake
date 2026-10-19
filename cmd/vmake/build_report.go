package main

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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/gitcmd"
	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/version"
)

const (
	reportCommandBuild   = "build"
	reportCommandRebuild = "rebuild"
	reportCommandTest    = "test"

	buildWarningCode       = "VMAKE_W_TOOL"
	buildWarningLimit      = 50
	buildStderrCaptureSize = 256 * 1024

	buildReportFile = "vmake-build.json"
	testReportFile  = "vmake-test.json"
)

type buildArtifact struct {
	Package     string `json:"package"`
	Target      string `json:"target"`
	BuildPath   string `json:"buildPath"`
	InstallPath string `json:"installPath"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type buildSummary struct {
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

type buildTestResult struct {
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

type buildReport struct {
	completed       bool
	location        string
	warningCut      func()
	SchemaVersion   int                       `json:"schemaVersion"`
	BuildID         string                    `json:"buildId"`
	Root            string                    `json:"root"`
	Status          string                    `json:"status"`
	Command         string                    `json:"command,omitempty"`
	VMakeVersion    string                    `json:"vmakeVersion,omitempty"`
	VMakeCommit     string                    `json:"vmakeCommit,omitempty"`
	ProjectRevision string                    `json:"projectRevision,omitempty"`
	ProjectDirty    *bool                     `json:"projectDirty,omitempty"`
	Started         string                    `json:"started"`
	Finished        string                    `json:"finished"`
	DurationMs      int64                     `json:"durationMs"`
	Error           string                    `json:"error"`
	Diagnostics     []buildDiagnostic         `json:"diagnostics"`
	Summary         *buildSummary             `json:"summary,omitempty"`
	ConfigFile      string                    `json:"configFile"`
	ConfigSHA256    string                    `json:"configSha256"`
	Global          map[string]any            `json:"global"`
	Packages        map[string]map[string]any `json:"packages"`
	Toolchain       string                    `json:"toolchain"`
	Mode            string                    `json:"mode"`
	CompileDatabase string                    `json:"compileDatabase"`
	Targets         []build.TargetResult      `json:"targets"`
	Tests           []buildTestResult         `json:"tests"`
	Artifacts       []buildArtifact           `json:"artifacts"`
}

type buildSummaryError struct {
	summary string
	err     error
}

func (e *buildSummaryError) Error() string { return e.summary }

func (e *buildSummaryError) Unwrap() error { return e.err }

func withBuildReport(run func(*buildReport) error) error {
	return withReport(reportCommandBuild, run)
}

func withReport(command string, run func(*buildReport) error) (err error) {
	report, err := beginBuildReport(command)
	if err != nil {
		return err
	}
	capture, stopCapture := startBuildStderrCapture()
	report.warningCut = func() {
		capture.cut(stopCapture)
		if jsonOutput && !vlog.IsQuiet() {
			os.Stdout = os.Stderr
		}
	}
	stdout := os.Stdout
	restoreStdout := func() {}
	if vlog.IsQuiet() {
		if devNull, openErr := os.OpenFile(os.DevNull, os.O_WRONLY, 0); openErr == nil {
			os.Stdout = devNull
			restored := false
			restoreStdout = func() {
				if restored {
					return
				}
				restored = true
				os.Stdout = stdout
				devNull.Close()
			}
		}
	} else if jsonOutput {
		os.Stdout = os.Stderr
		restored := false
		restoreStdout = func() {
			if restored {
				return
			}
			restored = true
			os.Stdout = stdout
		}
	}
	defer func() {
		defer restoreStdout()
		stopCapture()
		report.finish(&err, capture.warnings())
		restoreStdout()
		if jsonOutput {
			if writeErr := writeBuildJSONEnvelope(stdout, report); writeErr != nil {
				err = errors.Join(err, writeErr)
			}
		} else {
			printBuildSummary(stdout, report, err)
		}
		if err != nil && report.Error != "" {
			err = &buildSummaryError{summary: report.Error, err: err}
		}
	}()
	return run(report)
}

type buildStderrCapture struct {
	mu      sync.Mutex
	buffer  []byte
	frozen  []buildDiagnostic
	cutDone bool
}

func (c *buildStderrCapture) append(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(data) >= buildStderrCaptureSize {
		c.buffer = append(c.buffer[:0], data[len(data)-buildStderrCaptureSize:]...)
		return
	}
	c.buffer = append(c.buffer, data...)
	if len(c.buffer) > buildStderrCaptureSize {
		copy(c.buffer, c.buffer[len(c.buffer)-buildStderrCaptureSize:])
		c.buffer = c.buffer[:buildStderrCaptureSize]
	}
}

func (c *buildStderrCapture) cut(stop func()) {
	c.mu.Lock()
	if c.cutDone {
		c.mu.Unlock()
		return
	}
	c.cutDone = true
	c.mu.Unlock()
	stop()
	c.mu.Lock()
	c.frozen = parseBuildWarnings(string(c.buffer))
	c.buffer = nil
	c.mu.Unlock()
}

func (c *buildStderrCapture) warnings() []buildDiagnostic {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cutDone {
		return c.frozen
	}
	return parseBuildWarnings(string(c.buffer))
}

func startBuildStderrCapture() (*buildStderrCapture, func()) {
	capture := &buildStderrCapture{}
	reader, writer, err := os.Pipe()
	if err != nil {
		return capture, func() {}
	}
	stderr := os.Stderr
	os.Stderr = writer
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer reader.Close()
		buffer := make([]byte, 32*1024)
		for {
			n, readErr := reader.Read(buffer)
			if n > 0 {
				capture.append(buffer[:n])
				if _, writeErr := stderr.Write(buffer[:n]); writeErr != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()
	var once sync.Once
	return capture, func() {
		once.Do(func() {
			os.Stderr = stderr
			writer.Close()
			<-done
		})
	}
}

var (
	gnuWarningPattern  = regexp.MustCompile(`^\s*(.*?):(\d+):(?:(\d+):)?\s*warning:\s*(.*)$`)
	toolWarningPattern = regexp.MustCompile(`^\s*(?:([^\s:]+):\s*)?warning:\s*(.+)$`)
)

func parseBuildWarnings(output string) []buildDiagnostic {
	warnings := []buildDiagnostic{}
	seen := map[string]bool{}
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		warning, ok := parseBuildWarning(line)
		if !ok {
			if severity, file, lineNumber, isCMake := parseCMakeDiagnosticHeader(line); isCMake && severity == "warning" {
				warning = buildDiagnostic{
					Severity: "warning",
					Code:     buildWarningCode,
					Message:  cmakeDiagnosticMessage(lines, i, severity, file, lineNumber),
					File:     file,
					Line:     lineNumber,
				}
				ok = true
			} else if severity, isCMake := parseCMakeBareHeader(line); isCMake && severity == "warning" {
				warning = buildDiagnostic{
					Severity: "warning",
					Code:     buildWarningCode,
					Message:  cmakeBareDiagnosticMessage(lines, i, severity),
				}
				ok = true
			}
		}
		if !ok {
			continue
		}
		key := warningDiagnosticKey(warning)
		if seen[key] {
			continue
		}
		seen[key] = true
		warnings = append(warnings, warning)
		if len(warnings) == buildWarningLimit {
			break
		}
	}
	return warnings
}

func parseBuildWarning(line string) (buildDiagnostic, bool) {
	if match := gnuWarningPattern.FindStringSubmatch(line); match != nil {
		file := strings.TrimSpace(match[1])
		message := strings.TrimSpace(match[4])
		if file != "" && message != "" {
			lineNumber, _ := strconv.Atoi(match[2])
			column, _ := strconv.Atoi(match[3])
			return buildDiagnostic{
				Severity: "warning",
				Code:     buildWarningCode,
				Message:  truncateRunes(message, buildDiagnosticMessageLimit),
				File:     file,
				Line:     lineNumber,
				Column:   column,
			}, true
		}
	}
	if match := toolWarningPattern.FindStringSubmatch(line); match != nil {
		message := strings.TrimSpace(match[2])
		if match[1] != "" {
			message = match[1] + ": " + message
		}
		if message != "" {
			return buildDiagnostic{
				Severity: "warning",
				Code:     buildWarningCode,
				Message:  truncateRunes(message, buildDiagnosticMessageLimit),
			}, true
		}
	}
	return buildDiagnostic{}, false
}

func warningDiagnosticKey(diagnostic buildDiagnostic) string {
	return diagnostic.File + "\x00" + strconv.Itoa(diagnostic.Line) + "\x00" +
		strconv.Itoa(diagnostic.Column) + "\x00" + diagnostic.Message
}

func mergeBuildDiagnostics(diagnostics, warnings []buildDiagnostic) []buildDiagnostic {
	merged := make([]buildDiagnostic, 0, len(diagnostics)+len(warnings))
	pending := make([]buildDiagnostic, 0, len(warnings))
	seen := make(map[string]bool, len(warnings))
	addWarning := func(diagnostic buildDiagnostic) {
		if len(pending) == buildWarningLimit {
			return
		}
		diagnostic.Severity = "warning"
		diagnostic.Code = buildWarningCode
		key := warningDiagnosticKey(diagnostic)
		if seen[key] {
			return
		}
		seen[key] = true
		pending = append(pending, diagnostic)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "warning" {
			addWarning(diagnostic)
			continue
		}
		merged = append(merged, diagnostic)
	}
	for _, warning := range warnings {
		addWarning(warning)
	}
	return append(merged, pending...)
}

func printBuildSummary(writer io.Writer, report *buildReport, err error) {
	if report.Command == reportCommandTest {
		printTestSummary(writer, report, err)
		return
	}
	if err != nil {
		message := report.Error
		if message == "" {
			message = firstErrorLine(err)
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
			summary.ArtifactsInstalled, formatBuildDuration(summary.DurationMs), report.BuildID)
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
		formatBuildDuration(summary.DurationMs), report.BuildID)
}

func printTestSummary(writer io.Writer, report *buildReport, err error) {
	if err != nil {
		message := report.Error
		if message == "" {
			message = firstErrorLine(err)
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
		summary.TestsPassed, summary.TestsFound, formatBuildDuration(summary.DurationMs), report.BuildID)
}

func formatBuildDuration(durationMs int64) string {
	if durationMs < 1000 {
		return fmt.Sprintf("%dms", durationMs)
	}
	return fmt.Sprintf("%.1fs", float64(durationMs)/1000)
}

func reportPath(root string) string { return filepath.Join(root, "build", buildReportFile) }

func testReportPath(root string) string { return filepath.Join(root, "build", testReportFile) }

func reportPathFor(root, command string) string {
	if command == reportCommandTest {
		return testReportPath(root)
	}
	return reportPath(root)
}

func (r *buildReport) reportPath() string {
	if r.location != "" {
		return r.location
	}
	return reportPathFor(r.Root, r.Command)
}

func beginBuildReport(command string) (*buildReport, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	root := findProjectDir()
	r := &buildReport{SchemaVersion: 2, BuildID: hex.EncodeToString(id), Root: root, Status: "running", Command: command, VMakeVersion: version.Version, VMakeCommit: version.GitCommit, Started: time.Now().UTC().Format(time.RFC3339Nano), Targets: []build.TargetResult{}, Tests: []buildTestResult{}, Artifacts: []buildArtifact{}, Diagnostics: []buildDiagnostic{}, Global: map[string]any{}, Packages: map[string]map[string]any{}, location: reportPathFor(root, command)}
	r.ProjectRevision, r.ProjectDirty = projectProvenance(root)
	return r, jsonio.Save(r.reportPath(), r)
}

// projectProvenance records the project worktree revision at report start.
// Git is optional: any failure (not a worktree, unsupported checkout, missing
// git) omits the fields instead of affecting the build.
func projectProvenance(root string) (string, *bool) {
	topLevel, err := iexec.RunWithOptions("git", gitcmd.Args("rev-parse", "--show-toplevel"), iexec.RunOptions{Dir: root, Quiet: true})
	if err != nil {
		return "", nil
	}
	topDir := strings.TrimSpace(string(topLevel))
	rootAbs, rootErr := filepath.EvalSymlinks(root)
	topAbs, topErr := filepath.EvalSymlinks(topDir)
	if rootErr != nil || topErr != nil || filepath.Clean(rootAbs) != filepath.Clean(topAbs) {
		return "", nil
	}
	revision, err := iexec.RunWithOptions("git", gitcmd.Args("rev-parse", "HEAD"), iexec.RunOptions{Dir: root, Quiet: true})
	if err != nil {
		return "", nil
	}
	head := strings.TrimSpace(string(revision))
	if head == "" {
		return "", nil
	}
	status, err := iexec.RunWithOptions("git", gitcmd.Args(projectStatusArgs()...), iexec.RunOptions{
		Dir:   root,
		Quiet: true,
		Env:   map[string]string{"GIT_OPTIONAL_LOCKS": "0"},
	})
	if err != nil {
		return "", nil
	}
	dirty := strings.TrimSpace(string(status)) != ""
	return head, &dirty
}

// projectStatusArgs ignores vmake's own state and output directories so a
// checkout stays clean even when the project does not gitignore them. Every
// other change, including untracked source, still marks the report dirty.
func projectStatusArgs() []string {
	args := []string{"status", "--porcelain", "--", "."}
	for _, path := range []string{
		"build",
		"install",
		".vmake/_locks",
		".vmake/layout",
		".vmake/vmake.lock",
		".vmake_deps",
		"vmake_deps",
	} {
		args = append(args, ":(exclude)"+path)
	}
	return args
}

func (r *buildReport) finish(result *error, warnings []buildDiagnostic) {
	interrupted := recover()
	if interrupted != nil {
		*result = fmt.Errorf("build panicked: %v", interrupted)
	}
	if *result == nil && !r.completed {
		*result = fmt.Errorf("build interrupted before results were collected")
	}
	r.Finished = time.Now().UTC().Format(time.RFC3339Nano)
	r.DurationMs = reportDuration(r.Started, r.Finished)
	diagnostics, summary := buildFailureDetails(*result)
	if *result != nil && r.Command == reportCommandTest && !errors.Is(*result, context.Canceled) && !hasErrorDiagnostic(diagnostics) {
		diagnostics = append(diagnostics, buildDiagnostic{Severity: "error", Code: "VMAKE_E_TEST", Message: summary})
	}
	r.Diagnostics, r.Error = mergeBuildDiagnostics(diagnostics, warnings), summary
	r.Status = "succeeded"
	if *result != nil {
		if errors.Is(*result, context.Canceled) {
			r.Status = "cancelled"
		} else {
			r.Status = "failed"
		}
		r.Artifacts = []buildArtifact{}
	}
	if r.Summary != nil {
		r.Summary.DurationMs = r.DurationMs
		if r.Status == "failed" {
			r.Summary.NoOp = false
		}
	}
	if r.Tests == nil {
		r.Tests = []buildTestResult{}
	}
	r.setCompileDatabase()
	*result = errors.Join(*result, jsonio.Save(r.reportPath(), r))
	if interrupted != nil {
		panic(interrupted)
	}
}

func (r *buildReport) cutWarnings() {
	if r.warningCut == nil {
		return
	}
	cut := r.warningCut
	r.warningCut = nil
	cut()
}

func hasErrorDiagnostic(diagnostics []buildDiagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}

func (r *buildReport) setCompileDatabase() {
	path := filepath.Join(r.Root, "build", "compile_commands.json")
	if _, err := os.Stat(path); err == nil {
		r.CompileDatabase = path
	}
}

func reportDuration(started, finished string) int64 {
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

func fileDigest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func (r *buildReport) bind(ctx *RuntimeContext) error {
	r.ConfigFile, r.ConfigSHA256 = filepath.Base(ctx.ConfigPath), ctx.ConfigDigest
	r.Toolchain = pipeline.ResolveToolchainName(ctx.Config, ctx.ToolchainOverride)
	r.Mode = pipeline.ResolveMode(ctx.Config, ctx.ModeOverride)
	r.Global = effectiveGlobalValues(ctx)
	r.Global[api.ModeOptionName] = r.Mode
	r.Global[api.ToolchainOptionName] = r.Toolchain
	return jsonio.Save(r.reportPath(), r)
}

// partial records the results a failed build collected before it stopped.
// Artifacts, installation and completion stay untouched.
func (r *buildReport) partial(ctx *RuntimeContext, result *BuildResult, err error) {
	r.Global, r.Targets = result.GlobalValues, result.Targets
	r.Toolchain, r.Mode = result.TcName, result.Mode
	if r.Global == nil {
		r.Global = effectiveGlobalValues(ctx)
		r.Global[api.ModeOptionName] = r.Mode
		r.Global[api.ToolchainOptionName] = r.Toolchain
	}
	r.setCompileDatabase()
	for name, buildContext := range result.BuildCtxs {
		r.Packages[name] = maps.Clone(buildContext.CfgVals)
	}
	r.Summary = newBuildSummary(result, 0)
	r.Summary.TargetsFailed = failedTargetCount(err)
	r.Summary.NoOp = false
}

func failedTargetCount(err error) int {
	diagnostics, _ := buildFailureDetails(err)
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

// effectiveGlobalValues mirrors the pipeline's project-wide value resolution so
// a report published before (or without) a successful build still carries the
// configuration identity.
func effectiveGlobalValues(ctx *RuntimeContext) map[string]any {
	values := make(map[string]any)
	for name, option := range ctx.GlobalOptions {
		values[name] = option.Default()
	}
	for name, value := range config.BuildGlobalValues(ctx.Config) {
		if value == nil {
			continue
		}
		if value == "" && (name == api.TargetOSOptionName || name == api.TargetTripleOptionName) {
			continue
		}
		values[name] = value
	}
	return values
}

func (r *buildReport) setTestResults(tests []testResult) {
	r.Tests = make([]buildTestResult, len(tests))
	passed, failed := 0, 0
	for i, test := range tests {
		r.Tests[i] = buildTestResult{
			Name:       test.fullName,
			Package:    test.packageName,
			Target:     test.targetName,
			Passed:     test.passed,
			NotRun:     test.notRun,
			SkipReason: test.skipReason,
			ElapsedMs:  test.elapsed.Milliseconds(),
			ExitCode:   test.exitCode,
			Signal:     test.signal,
			Output:     test.output,
		}
		if test.notRun {
			continue
		}
		if test.passed {
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

func (r *buildReport) complete(ctx *RuntimeContext, result *BuildResult) error {
	r.ConfigFile, r.ConfigSHA256 = filepath.Base(ctx.ConfigPath), ctx.ConfigDigest
	_, path, digest, err := config.LoadProjectSnapshot(r.Root)
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
		r.Packages[name] = maps.Clone(buildContext.CfgVals)
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
		hash, size, err := fileDigest(source)
		if err != nil {
			return err
		}
		installedHash, _, err := fileDigest(installed)
		if err != nil {
			return err
		}
		if installedHash != hash {
			return fmt.Errorf("installed artifact differs: %s", installed)
		}
		seen[key] = true
		r.Artifacts = append(r.Artifacts, buildArtifact{file.Package, file.Target, source, installed, size, hash})
	}
	slices.SortFunc(r.Artifacts, func(a, b buildArtifact) int {
		if c := strings.Compare(a.BuildPath, b.BuildPath); c != 0 {
			return c
		}
		return strings.Compare(a.InstallPath, b.InstallPath)
	})
	r.Summary = newBuildSummary(result, len(r.Artifacts))
	r.completed = true
	return nil
}

func newBuildSummary(result *BuildResult, artifacts int) *buildSummary {
	changedFiles := result.Stats.CompiledFiles
	if changedFiles == nil {
		changedFiles = []string{}
	}
	summary := &buildSummary{
		TargetsTotal:       len(result.Targets),
		FilesCompiled:      result.Stats.Compiled,
		FilesLinked:        result.Stats.Linked,
		ActivitiesUpToDate: result.Stats.UpToDate,
		ArtifactsInstalled: artifacts,
		ChangedFiles:       changedFiles,
		ExternalStages:     result.ExternalStages(),
	}
	for _, target := range result.Targets {
		switch target.Status {
		case build.TargetStatusBuilt:
			summary.TargetsBuilt++
			if target.Kind == string(api.TargetVoid) {
				summary.Callbacks++
			}
		case build.TargetStatusUpToDate:
			summary.TargetsUpToDate++
		}
	}
	summary.NoOp = summary.FilesCompiled == 0 && summary.FilesLinked == 0 &&
		len(summary.ChangedFiles) == 0 && summary.ExternalStages == 0
	return summary
}

func invalidateBuildReport() error {
	root := findProjectDir()
	return errors.Join(removeReport(reportPath(root)), removeReport(testReportPath(root)))
}

func removeReport(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
