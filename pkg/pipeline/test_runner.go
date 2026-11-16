package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	exec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

type TestResult struct {
	FullName    string
	PackageName string
	TargetName  string
	OutputPath  string
	Passed      bool
	NotRun      bool
	SkipReason  string
	Elapsed     time.Duration
	ExitCode    int
	Signal      string
	Output      string
}

const (
	BuildTestOutputLimit  = 4096
	BuildTestOutputMarker = "...[truncated]\n"
)

var ErrNoTestTargets = errors.New("no test targets found")

func ValidateTestExecution(platform api.Platform, hostOS string) error {
	targetOS := platform.OSOrHost()
	if targetOS == "none" || targetOS != hostOS || platform.Triple != "" {
		return fmt.Errorf("cannot run tests for project target (target_os=%s, target_triple=%q) on %s; use 'vmake build --tests' to build them without execution", targetOS, platform.Triple, hostOS)
	}
	return nil
}

func ValidateTestProject(ctx *RuntimeContext) error {
	needed, err := ComputeNeeded(ctx)
	if err != nil {
		return err
	}
	roots := needed.Roots
	sort.Strings(roots)
	if len(roots) == 0 {
		platform, err := ProjectPlatform(ctx)
		if err != nil {
			return err
		}
		return ValidateTestExecution(platform, runtime.GOOS)
	}
	var firstErr error
	for _, name := range roots {
		platform, err := PackagePlatform(ctx, name)
		if err != nil {
			return err
		}
		if err := ValidateTestExecution(platform, runtime.GOOS); err == nil {
			return nil
		} else if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func BoundedTestOutput(output string) string {
	output = strings.TrimSpace(output)
	if len(output) <= BuildTestOutputLimit {
		return output
	}
	keep := BuildTestOutputLimit - len(BuildTestOutputMarker)
	tail := output[len(output)-keep:]
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	return BuildTestOutputMarker + tail
}

func TestExitCode(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var exitErr *osexec.ExitError
	if errors.As(err, &exitErr) {
		if signal, ok := testProcessSignal(exitErr); ok {
			return -1, signal
		}
		if code := exitErr.ExitCode(); code >= 0 {
			return code, ""
		}
	}
	return 1, ""
}

func RunAllTests(ctx context.Context, result *BuildResult) ([]TestResult, error) {
	var tests []TestResult

	for _, fullName := range result.Graph.Order {
		node, err := result.Graph.GetNode(fullName)
		if err != nil {
			continue
		}
		if !node.Target.IsTest() || !node.Target.IsDefault() {
			continue
		}
		if node.Target.Kind() != api.TargetBinary {
			continue
		}

		layout, ok := result.Layouts[node.PkgName]
		if !ok || layout == nil {
			return nil, fmt.Errorf("test target %s has no resolved package layout", fullName)
		}
		platform := layout.Platform
		placement, ok := result.TargetPlacements[fullName]
		if !ok || placement.OutputPath == "" {
			return nil, fmt.Errorf("test target %s has no build placement", fullName)
		}
		outputPath := placement.OutputPath
		if err := ValidateTestExecution(platform, runtime.GOOS); err != nil {
			return nil, err
		}
		if _, err := os.Stat(outputPath); err != nil {
			vlog.Error("FAIL %s (binary not found: %s)", fullName, outputPath)
			return nil, fmt.Errorf("test binary missing: %w", err)
		}

		tests = append(tests, TestResult{
			FullName:    fullName,
			PackageName: node.PkgName,
			TargetName:  node.Target.Name(),
			OutputPath:  outputPath,
		})
	}

	if len(tests) == 0 {
		return nil, ErrNoTestTargets
	}

	vlog.Info("")
	vlog.Info("Running %d test(s)...", len(tests))

	for i := range tests {
		if err := ctx.Err(); err != nil {
			return tests[:i], err
		}
		t := &tests[i]
		vlog.Info("")
		vlog.Info("[%s]", t.FullName)

		start := time.Now()
		output, runErr := exec.RunWithOptions(t.OutputPath, nil, exec.RunOptions{
			Dir:           filepath.Dir(t.OutputPath),
			Context:       ctx,
			CaptureStderr: true,
		})
		if ctx.Err() != nil {
			t.Elapsed = time.Since(start)
			t.NotRun = true
			t.SkipReason = "interrupted"
			tests = append(tests[:i], *t)
			return tests, ctx.Err()
		}
		t.Elapsed = time.Since(start)
		t.Passed = runErr == nil
		t.ExitCode, t.Signal = TestExitCode(runErr)
		if commandErr, ok := runErr.(*exec.CommandError); ok {
			t.Output = BoundedTestOutput(commandErr.Output)
		} else {
			t.Output = BoundedTestOutput(string(output))
		}

		if t.Passed {
			vlog.Info("PASS %s (%s)", t.FullName, t.Elapsed.Round(time.Millisecond))
		} else {
			vlog.Error("FAIL %s (%s)", t.FullName, t.Elapsed.Round(time.Millisecond))
		}
	}

	vlog.Info("")
	failed := 0
	for _, test := range tests {
		if !test.Passed {
			failed++
		}
	}

	if failed > 0 {
		return tests, fmt.Errorf("%d test(s) failed", failed)
	}
	return tests, nil
}
