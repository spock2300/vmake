package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	cmake "github.com/spock2300/vmake/internal/cmake"
	"github.com/spock2300/vmake/internal/exec"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func (p *Package) cmakeSettings() cmake.Settings {
	return p.cmakeSettingsForHost(runtime.GOOS)
}

func (p *Package) cmakeSettingsForHost(hostOS string) cmake.Settings {
	return cmake.Settings{
		SourceDir:    p.SrcDir(),
		BuildRoot:    p.dirs.BuildDir,
		BuildDir:     p.CMakeBuildDir(),
		InstallDir:   p.CMakeInstallDir(),
		BuildType:    p.cmakeBuildType(),
		HostOS:       hostOS,
		TargetOS:     p.TargetOS(),
		TargetTriple: p.TargetTriple(),
		Toolchain:    p.tc,
		GlobalFlags:  p.cmakeGlobalFlagsArgs(hostOS),
		DryRun:       p.dryRun,
	}
}

func (p *Package) SetCMakeBuildDir(dir string) *Package {
	if dir == "" {
		fatalScript(p.name, "SetCMakeBuildDir", "directory must not be empty")
	}
	p.cmakeBuildDir = dir
	return p
}

func (p *Package) SetCMakeInstallDir(dir string) *Package {
	if dir == "" {
		fatalScript(p.name, "SetCMakeInstallDir", "directory must not be empty")
	}
	p.cmakeInstallDir = dir
	return p
}

func (p *Package) SetCMakeBuildType(config string) *Package {
	if strings.TrimSpace(config) == "" {
		fatalScript(p.name, "SetCMakeBuildType", "build type must not be empty")
	}
	p.cmakeConfig = config
	return p
}

func (p *Package) CMakeBuildDir() string {
	if p.cmakeBuildDir == "" {
		return p.BuildPath("cmake")
	}
	return p.cmakeDir(p.cmakeBuildDir)
}

func (p *Package) CMakeInstallDir() string {
	if p.cmakeInstallDir != "" {
		return p.cmakeDir(p.cmakeInstallDir)
	}
	if p.dirs.InstallDir != "" {
		return p.dirs.InstallDir
	}
	return p.BuildPath("staging")
}

func (p *Package) CMakeGlobalFlagsArgs() []string {
	return p.cmakeGlobalFlagsArgs(runtime.GOOS)
}

func (p *Package) cmakeGlobalFlagsArgs(hostOS string) []string {
	var args []string
	if cf := p.mergedCFlags(hostOS); cf != "" {
		args = append(args, "-DCMAKE_C_FLAGS="+cf)
	}
	if cxxf := p.mergedCxxFlags(hostOS); cxxf != "" {
		args = append(args, "-DCMAKE_CXX_FLAGS="+cxxf)
	}
	if ldf := p.mergedLdFlags(hostOS); ldf != "" {
		args = append(args,
			"-DCMAKE_EXE_LINKER_FLAGS="+ldf,
			"-DCMAKE_SHARED_LINKER_FLAGS="+ldf,
			"-DCMAKE_MODULE_LINKER_FLAGS="+ldf,
		)
	}
	return args
}

func (p *Package) CMakeConfigure(extraArgs ...string) {
	engine := cmake.NewEngine(p.cmakeSettings())
	plan, err := engine.PlanConfigure(extraArgs)
	if err != nil {
		fatalScript(p.name, "CMakeConfigure", "%v", err)
	}
	if plan.Reuse {
		vlog.Info("  cmake configure (cached): %s", filepath.ToSlash(p.CMakeBuildDir()))
		return
	}
	if p.DryRun() {
		p.runCMakeConfigure(plan)
		return
	}
	if err := engine.Invalidate(); err != nil {
		vlog.Debug("  cmake configure: remove stale stamp: %v", err)
	}
	p.runCapturedCMake("CMakeConfigure", "configure", plan)
	if err := engine.Commit(plan); err != nil {
		vlog.Debug("  cmake configure: record fingerprint: %v", err)
	}
}

func (p *Package) runCMakeConfigure(plan cmake.Plan) {
	if plan.Preset {
		p.RunIn(plan.Dir, plan.Executable, plan.Args...)
		return
	}
	p.Run(plan.Executable, plan.Args...)
}

func (p *Package) CMakeBuild(extraArgs ...string) {
	extra, err := p.executionBudget().CMakeBuildArgs(extraArgs, parallelEnvironment())
	if err != nil {
		fatalScript(p.name, "CMakeBuild", "%v", err)
	}
	plan, err := cmake.NewEngine(p.cmakeSettings()).PlanBuild(extra)
	if err != nil {
		fatalScript(p.name, "CMakeBuild", "%v", err)
	}
	p.runCapturedCMake("CMakeBuild", "build", plan)
}

func (p *Package) CMakeInstall(extraArgs ...string) {
	extra, err := p.executionBudget().CMakeInstallArgs(extraArgs, parallelEnvironment())
	if err != nil {
		fatalScript(p.name, "CMakeInstall", "%v", err)
	}
	plan, err := cmake.NewEngine(p.cmakeSettings()).PlanInstall(extra)
	if err != nil {
		fatalScript(p.name, "CMakeInstall", "%v", err)
	}
	p.runCapturedCMake("CMakeInstall", "install", plan)
}

// runCapturedCMake runs one cmake stage with captured output. Success prints a
// single summary line plus non-progress lines at default verbosity and the full
// log under -v; a failure keeps the whole captured log in the script error.
// Warning and error lines always stream to stderr so the invocation's warning
// capture can turn them into diagnostics. A long stage also prints a periodic
// stderr heartbeat, including under -q.
func (p *Package) runCapturedCMake(op, phase string, plan cmake.Plan) {
	if p.DryRun() {
		p.logAndDryRun(plan.Executable, plan.Args)
		return
	}
	p.executedHelperStages++
	if err := os.MkdirAll(plan.Dir, 0755); err != nil {
		fatalScript(p.name, op, "create cmake %s directory %q: %v", phase, plan.Dir, err)
	}
	output, err := p.runCMakeCaptured(phase, plan.Dir, plan.Executable, plan.Args)
	summary, detail := renderCMakeOutput(phase, output)
	if err != nil {
		printCMakeDetail(detail, vlog.IsQuiet())
		for _, line := range splitCMakeLines(output) {
			vlog.Verbose("%s", line)
		}
		fatalScript(p.name, op, "%v", err)
	}
	vlog.Info("%s", summary)
	printCMakeDetail(detail, false)
	for _, line := range splitCMakeLines(output) {
		vlog.Verbose("%s", line)
	}
}

// printCMakeDetail streams default-visible CMake detail. Warning/error lines
// and their indented continuation lines go to stderr; allErrors sends every
// line there too (the quiet failure path). A diagnostic block ends with a
// blank stderr line so consumers can delimit continuation lines even when the
// captured output drops CMake's own separators.
func printCMakeDetail(lines []string, allErrors bool) {
	inDiagnostic := false
	endDiagnostic := func() {
		if inDiagnostic {
			vlog.Error("")
			inDiagnostic = false
		}
	}
	for _, line := range lines {
		switch {
		case cmakeDiagnosticLine(line):
			inDiagnostic = true
		case line == "" || line[0] != ' ' && line[0] != '\t':
			endDiagnostic()
		}
		if inDiagnostic || allErrors {
			vlog.Error("%s", line)
		} else {
			vlog.Info("%s", line)
		}
	}
	endDiagnostic()
}

var cmakeHeartbeatInterval = exec.DefaultProgressInterval

func (p *Package) runCMakeCaptured(phase, dir, executable string, args []string) (string, error) {
	env := p.tc.CommandEnv()
	output, err := exec.RunWithOptions(executable, args, exec.RunOptions{
		Dir:              dir,
		Env:              env,
		Context:          p.executionContext(),
		Quiet:            true,
		ProgressInterval: cmakeHeartbeatInterval,
		OnProgress: func(elapsed time.Duration) {
			vlog.Error("  cmake %s: still running (%s)", phase, elapsed.Round(100*time.Millisecond))
		},
	})
	if err != nil {
		var commandErr *exec.CommandError
		if errors.As(err, &commandErr) {
			return commandErr.Output, err
		}
		return string(output), err
	}
	return string(output), nil
}

var cmakeProgressLine = regexp.MustCompile(`^(?:\[\d+/\d+\]|\[\s*\d+%\])`)

// renderCMakeOutput reports the one-line summary and the default-visible
// detail lines for a captured configure/build/install stage. Ninja progress and
// CMake up-to-date chatter are counted but hidden; install scaffolding is
// hidden. Configure hides its "-- ..." progress scaffolding while keeping
// warning and error lines.
func renderCMakeOutput(phase, output string) (string, []string) {
	var steps, installed, upToDate int
	var detail []string
	for _, line := range splitCMakeLines(output) {
		switch {
		case cmakeProgressLine.MatchString(line):
			steps++
		case strings.HasPrefix(line, "ninja: no work to do"):
		case strings.HasPrefix(line, "-- Up-to-date:"):
			upToDate++
		case strings.HasPrefix(line, "-- Install configuration:"):
		case strings.HasPrefix(line, "-- Installing:"):
			installed++
			detail = append(detail, line)
		case phase == "configure" && strings.HasPrefix(line, "-- ") && !cmakeDiagnosticLine(line):
		default:
			detail = append(detail, line)
		}
	}
	if phase == "configure" {
		return "  cmake configure: done", detail
	}
	if phase == "install" {
		return fmt.Sprintf("  cmake install: %d installed, %d up to date", installed, upToDate), detail
	}
	switch steps {
	case 0:
		return "  cmake build: up to date", detail
	case 1:
		return "  cmake build: 1 step", detail
	}
	return fmt.Sprintf("  cmake build: %d steps", steps), detail
}

func cmakeDiagnosticLine(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "warning:") || strings.Contains(lower, "cmake warning") ||
		strings.Contains(lower, "cmake deprecation warning") || strings.Contains(lower, "cmake dev warning") ||
		strings.Contains(lower, "cmake author warning") || strings.Contains(lower, "error:") ||
		strings.Contains(lower, "cmake error")
}

func splitCMakeLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSuffix(line, "\r"); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func (p *Package) cmakeDir(dir string) string {
	if filepath.IsAbs(dir) {
		return dir
	}
	return p.BuildPath(dir)
}

func (p *Package) cmakeBuildType() string {
	if p.cmakeConfig != "" {
		return p.cmakeConfig
	}
	m, _ := p.cfgVals[ModeOptionName].(string)
	switch m {
	case ModeDebug:
		return "Debug"
	case ModeSize:
		return "MinSizeRel"
	default:
		return "Release"
	}
}
