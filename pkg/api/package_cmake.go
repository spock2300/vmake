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
	"github.com/spock2300/vmake/pkg/toolchain"
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
		fatalScript(p.Name, "SetCMakeBuildDir", "directory must not be empty")
	}
	p.cmakeBuildDir = dir
	return p
}

func (p *Package) SetCMakeInstallDir(dir string) *Package {
	if dir == "" {
		fatalScript(p.Name, "SetCMakeInstallDir", "directory must not be empty")
	}
	p.cmakeInstallDir = dir
	return p
}

func (p *Package) SetCMakeBuildType(config string) *Package {
	if strings.TrimSpace(config) == "" {
		fatalScript(p.Name, "SetCMakeBuildType", "build type must not be empty")
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
	args, err := p.cmakeConfigureArgs(runtime.GOOS, extraArgs...)
	if err != nil {
		fatalScript(p.Name, "CMakeConfigure", "%v", err)
	}
	fingerprint, cached := p.cmakeConfigureState(args)
	if cached {
		vlog.Info("  cmake configure (cached): %s", filepath.ToSlash(p.CMakeBuildDir()))
		return
	}
	if p.DryRun() {
		p.runCMakeConfigure(args, extraArgs)
		return
	}
	// A failed or interrupted configure may have mutated CMakeCache.txt and
	// CMakeFiles. Drop the previous stamp before running so a later invocation
	// cannot reuse a tree that no longer matches it.
	if err := os.Remove(p.cmakeConfigureStampPath()); err != nil && !os.IsNotExist(err) {
		vlog.Debug("  cmake configure: remove stale stamp: %v", err)
	}
	p.runCapturedCMake("CMakeConfigure", "configure", p.cmakeConfigureDir(extraArgs), args)
	if err := p.saveCMakeConfigureStamp(args, fingerprint); err != nil {
		vlog.Debug("  cmake configure: record fingerprint: %v", err)
	}
}

func (p *Package) cmakeConfigureDir(extraArgs []string) string {
	return cmake.ConfigureDir(p.cmakeSettings(), extraArgs)
}

func (p *Package) runCMakeConfigure(args, extraArgs []string) {
	if hasCMakeOption(extraArgs, "--preset") {
		p.RunIn(p.SrcDir(), "cmake", args...)
	} else {
		p.Run("cmake", args...)
	}
}

func (p *Package) CMakeBuild(extraArgs ...string) {
	args, err := p.cmakeBuildArgs(extraArgs...)
	if err != nil {
		fatalScript(p.Name, "CMakeBuild", "%v", err)
	}
	p.runCapturedCMake("CMakeBuild", "build", p.dirs.BuildDir, args)
}

func (p *Package) CMakeInstall(extraArgs ...string) {
	args, err := p.cmakeInstallArgs(extraArgs...)
	if err != nil {
		fatalScript(p.Name, "CMakeInstall", "%v", err)
	}
	p.runCapturedCMake("CMakeInstall", "install", p.dirs.BuildDir, args)
}

// runCapturedCMake runs one cmake stage with captured output. Success prints a
// single summary line plus non-progress lines at default verbosity and the full
// log under -v; a failure keeps the whole captured log in the script error.
// Warning and error lines always stream to stderr so the invocation's warning
// capture can turn them into diagnostics. A long stage also prints a periodic
// stderr heartbeat, including under -q.
func (p *Package) runCapturedCMake(op, phase, dir string, args []string) {
	if p.DryRun() {
		p.logAndDryRun("cmake", args)
		return
	}
	p.executedHelperStages++
	if err := os.MkdirAll(dir, 0755); err != nil {
		fatalScript(p.Name, op, "create cmake %s directory %q: %v", phase, dir, err)
	}
	output, err := p.runCMakeCaptured(phase, dir, args)
	summary, detail := renderCMakeOutput(phase, output)
	if err != nil {
		printCMakeDetail(detail, vlog.IsQuiet())
		for _, line := range splitCMakeLines(output) {
			vlog.Verbose("%s", line)
		}
		fatalScript(p.Name, op, "%v", err)
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

func (p *Package) runCMakeCaptured(phase, dir string, args []string) (string, error) {
	name := "cmake"
	env := p.tc.CommandEnv()
	if p.tc != nil {
		for _, tool := range p.tc.Tools.Entries() {
			configured := *tool.Value
			if configured != "" && name == configured {
				path, err := toolchain.ResolveToolPath(configured, p.tc.InstallPath)
				if err != nil {
					return "", err
				}
				name = path
				break
			}
		}
	}
	output, err := exec.RunWithOptions(name, args, exec.RunOptions{
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
	m, _ := p.CfgVals[ModeOptionName].(string)
	switch m {
	case ModeDebug:
		return "Debug"
	case ModeSize:
		return "MinSizeRel"
	default:
		return "Release"
	}
}

func (p *Package) cmakeConfigureArgs(hostOS string, extraArgs ...string) ([]string, error) {
	return cmake.ConfigureArgs(p.cmakeSettingsForHost(hostOS), extraArgs...)
}

func (p *Package) cmakeBuildArgs(extraArgs ...string) ([]string, error) {
	if err := validateCMakeArgs(extraArgs); err != nil {
		return nil, err
	}
	if hasCMakeOption(extraArgs, "--preset") {
		return nil, fmt.Errorf("CMake build presets override the managed build directory; pass configure presets to CMakeConfigure and use --target or --config with CMakeBuild")
	}
	args := []string{"--build", filepath.ToSlash(p.CMakeBuildDir())}
	if !hasCMakeOption(extraArgs, "--config") {
		args = append(args, "--config", p.cmakeBuildType())
	}
	parallelArgs, err := p.executionBudget().CMakeBuildArgs(extraArgs, parallelEnvironment())
	if err != nil {
		return nil, err
	}
	return append(args, parallelArgs...), nil
}

func (p *Package) cmakeInstallArgs(extraArgs ...string) ([]string, error) {
	if err := validateCMakeArgs(extraArgs); err != nil {
		return nil, err
	}
	args := []string{"--install", filepath.ToSlash(p.CMakeBuildDir()), "--prefix", filepath.ToSlash(p.CMakeInstallDir())}
	if !hasCMakeOption(extraArgs, "--config") {
		args = append(args, "--config", p.cmakeBuildType())
	}
	parallelArgs, err := p.executionBudget().CMakeInstallArgs(extraArgs, parallelEnvironment())
	if err != nil {
		return nil, err
	}
	return append(args, parallelArgs...), nil
}

func validateCMakeArgs(args []string) error {
	return cmake.ValidateArgs(args)
}

func hasCMakeOption(args []string, options ...string) bool {
	return cmake.HasOption(args, options...)
}
