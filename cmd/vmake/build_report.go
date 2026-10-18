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
	"slices"
	"strings"
	"time"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
)

const (
	reportCommandBuild = "build"
	reportCommandTest  = "test"
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
}

type buildTestResult struct {
	Name      string `json:"name"`
	Package   string `json:"package"`
	Target    string `json:"target"`
	Passed    bool   `json:"passed"`
	ElapsedMs int64  `json:"elapsedMs"`
}

type buildReport struct {
	completed       bool
	SchemaVersion   int                       `json:"schemaVersion"`
	BuildID         string                    `json:"buildId"`
	Root            string                    `json:"root"`
	Status          string                    `json:"status"`
	Command         string                    `json:"command,omitempty"`
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

func withBuildReport(run func(*buildReport) error) (err error) {
	report, err := beginBuildReport()
	if err != nil {
		return err
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
	}
	defer func() {
		defer restoreStdout()
		report.finish(&err)
		restoreStdout()
		printBuildSummary(stdout, report, err)
		if err != nil && report.Error != "" {
			err = &buildSummaryError{summary: report.Error, err: err}
		}
	}()
	return run(report)
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
	fmt.Fprintf(writer, "Build succeeded: %d targets (%d built, %d up to date), %d compiled, %d linked, %d installed in %s (buildId=%s)\n",
		summary.TargetsTotal, summary.TargetsBuilt, summary.TargetsUpToDate,
		summary.FilesCompiled, summary.FilesLinked, summary.ArtifactsInstalled,
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
	fmt.Fprintf(writer, "Tests succeeded: %d/%d passed in %s (buildId=%s)\n",
		summary.TestsPassed, summary.TestsFound, formatBuildDuration(summary.DurationMs), report.BuildID)
}

func formatBuildDuration(durationMs int64) string {
	if durationMs < 1000 {
		return fmt.Sprintf("%dms", durationMs)
	}
	return fmt.Sprintf("%.1fs", float64(durationMs)/1000)
}

func reportPath(root string) string { return filepath.Join(root, "build", "vmake-build.json") }

func beginBuildReport() (*buildReport, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	r := &buildReport{SchemaVersion: 2, BuildID: hex.EncodeToString(id), Root: findProjectDir(), Status: "running", Command: reportCommandBuild, Started: time.Now().UTC().Format(time.RFC3339Nano), Targets: []build.TargetResult{}, Tests: []buildTestResult{}, Artifacts: []buildArtifact{}, Diagnostics: []buildDiagnostic{}, Global: map[string]any{}, Packages: map[string]map[string]any{}}
	return r, jsonio.Save(reportPath(r.Root), r)
}

func (r *buildReport) finish(result *error) {
	interrupted := recover()
	if interrupted != nil {
		*result = fmt.Errorf("build panicked: %v", interrupted)
	}
	if *result == nil && !r.completed {
		*result = fmt.Errorf("build interrupted before results were collected")
	}
	r.Finished = time.Now().UTC().Format(time.RFC3339Nano)
	r.DurationMs = reportDuration(r.Started, r.Finished)
	r.Diagnostics, r.Error = buildFailureDetails(*result)
	r.Status = "succeeded"
	if *result != nil {
		if errors.Is(*result, context.Canceled) {
			r.Status = "cancelled"
		} else {
			r.Status = "failed"
		}
		r.Artifacts = []buildArtifact{}
		if r.Command != reportCommandTest {
			r.Summary = nil
		}
	}
	if r.Summary != nil {
		r.Summary.DurationMs = r.DurationMs
	}
	if r.Tests == nil {
		r.Tests = []buildTestResult{}
	}
	r.setCompileDatabase()
	*result = errors.Join(*result, jsonio.Save(reportPath(r.Root), r))
	if interrupted != nil {
		panic(interrupted)
	}
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
	return jsonio.Save(reportPath(r.Root), r)
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
			Name:      test.fullName,
			Package:   test.packageName,
			Target:    test.targetName,
			Passed:    test.passed,
			ElapsedMs: test.elapsed.Milliseconds(),
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
	}
	for _, target := range result.Targets {
		switch target.Status {
		case build.TargetStatusBuilt:
			summary.TargetsBuilt++
		case build.TargetStatusUpToDate:
			summary.TargetsUpToDate++
		}
	}
	return summary
}

func invalidateBuildReport() error {
	err := os.Remove(reportPath(findProjectDir()))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
