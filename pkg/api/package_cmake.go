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

	"github.com/spock2300/vmake/internal/exec"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

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
	var args []string
	if cf := p.MergedCFlags(); cf != "" {
		args = append(args, "-DCMAKE_C_FLAGS="+cf)
	}
	if cxxf := p.MergedCxxFlags(); cxxf != "" {
		args = append(args, "-DCMAKE_CXX_FLAGS="+cxxf)
	}
	if ldf := p.MergedLdFlags(); ldf != "" {
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
	if hasCMakeOption(extraArgs, "--preset") {
		return p.SrcDir()
	}
	return p.dirs.BuildDir
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
	if err := validateCMakeArgs(extraArgs); err != nil {
		return nil, err
	}
	if p.tc == nil {
		return nil, fmt.Errorf("toolchain is not configured")
	}
	args := []string{
		"-S", filepath.ToSlash(p.SrcDir()),
		"-B", filepath.ToSlash(p.CMakeBuildDir()),
		"-DCMAKE_INSTALL_PREFIX=" + filepath.ToSlash(p.CMakeInstallDir()),
	}
	languages, languageList := cmakeProjectLanguageList(p.SrcDir())
	// Compiler and compiler-target arguments follow the project's declared
	// languages so CMake does not report vmake-provided variables it never
	// consumes; binutils stay unconditional. Without a parseable language list
	// every compiler is injected.
	for _, tool := range []struct{ key, language, name string }{
		{"CMAKE_C_COMPILER", "C", p.tc.Tools.CC},
		{"CMAKE_CXX_COMPILER", "CXX", p.tc.Tools.CXX},
		{"CMAKE_ASM_COMPILER", "ASM", p.tc.Tools.CC},
		{"CMAKE_AR", "", p.tc.Tools.AR},
		{"CMAKE_RANLIB", "", p.tc.Tools.RANLIB},
		{"CMAKE_STRIP", "", p.tc.Tools.STRIP},
		{"CMAKE_OBJCOPY", "", p.tc.Tools.OBJCOPY},
		{"CMAKE_NM", "", p.tc.Tools.NM},
	} {
		if tool.name == "" {
			continue
		}
		if languageList && tool.language != "" && !languages[tool.language] {
			continue
		}
		path, err := toolchain.ResolveToolPath(tool.name, p.tc.InstallPath)
		if err != nil {
			return nil, fmt.Errorf("resolve %s %q: %w", tool.key, tool.name, err)
		}
		args = append(args, "-D"+tool.key+"="+filepath.ToSlash(path))
	}
	args = append(args, "-DCMAKE_BUILD_TYPE="+p.cmakeBuildType())
	targetOS := p.TargetOS()
	if p.TargetTriple() != "" || targetOS != hostOS {
		systemNames := map[string]string{
			"none": "Generic", "linux": "Linux", "windows": "Windows",
			"darwin": "Darwin", "freebsd": "FreeBSD", "openbsd": "OpenBSD",
			"netbsd": "NetBSD", "android": "Android",
		}
		systemName, ok := systemNames[targetOS]
		if !ok {
			return nil, fmt.Errorf("unsupported CMake target OS %q", targetOS)
		}
		args = append(args, "-DCMAKE_SYSTEM_NAME="+systemName)
	}
	if targetOS == "none" {
		args = append(args,
			"-DCMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY",
			"-DCMAKE_FIND_ROOT_PATH_MODE_PROGRAM=NEVER",
			"-DCMAKE_FIND_ROOT_PATH_MODE_LIBRARY=ONLY",
			"-DCMAKE_FIND_ROOT_PATH_MODE_INCLUDE=ONLY",
		)
	}
	if p.TargetTriple() != "" {
		if !languageList || languages["C"] {
			args = append(args, "-DCMAKE_C_COMPILER_TARGET="+p.TargetTriple())
		}
		if !languageList || languages["CXX"] {
			args = append(args, "-DCMAKE_CXX_COMPILER_TARGET="+p.TargetTriple())
		}
		if !languageList || languages["ASM"] {
			args = append(args, "-DCMAKE_ASM_COMPILER_TARGET="+p.TargetTriple())
		}
	}
	if hostOS == "windows" && !hasCMakeGenerator(extraArgs) && os.Getenv("CMAKE_GENERATOR") == "" {
		args = append(args, "-G", "Ninja")
	}
	definitions := cmakeDefinitions(extraArgs)
	for _, arg := range p.CMakeGlobalFlagsArgs() {
		if languageList {
			if strings.HasPrefix(arg, "-DCMAKE_C_FLAGS=") && !languages["C"] {
				continue
			}
			if strings.HasPrefix(arg, "-DCMAKE_CXX_FLAGS=") && !languages["CXX"] {
				continue
			}
		}
		if !definitions[cmakeDefinitionKey(strings.TrimPrefix(arg, "-D"))] {
			args = append(args, arg)
		}
	}
	args = append(args, extraArgs...)
	return args, nil
}

// cmakeProjectLanguageList returns the languages declared by the source tree's
// project() command, parsing both the LANGUAGES form and the legacy
// project(<name> <language>...) form. ok is false when CMakeLists.txt is
// missing, has no project() call, or no language list is parseable; callers
// then keep the unconditional compiler injection.
func cmakeProjectLanguageList(srcDir string) (map[string]bool, bool) {
	data, err := os.ReadFile(filepath.Join(srcDir, "CMakeLists.txt"))
	if err != nil {
		return nil, false
	}
	stripped := stripCMakeComments(string(data))
	if _, ok := cmakeCommandArguments(stripped, "enable_language"); ok {
		return nil, false
	}
	body, ok := cmakeCommandArguments(stripped, "project")
	if !ok {
		return nil, false
	}
	args := cmakeArguments(body)
	for i, arg := range args {
		if arg != "LANGUAGES" {
			continue
		}
		languages := make(map[string]bool)
		for _, language := range args[i+1:] {
			if cmakeProjectKeywords[language] {
				break
			}
			if strings.ContainsAny(language, "${\"") {
				return nil, false
			}
			canonical, ok := cmakeLanguageNames[language]
			if !ok {
				return nil, false
			}
			languages[canonical] = true
		}
		if len(languages) == 0 {
			return nil, false
		}
		return languages, true
	}
	// The legacy form project(<name> <language>...) has no LANGUAGES keyword.
	if len(args) > 0 {
		languages := make(map[string]bool)
		for _, language := range args[1:] {
			canonical, ok := cmakeLanguageNames[language]
			if !ok {
				break
			}
			languages[canonical] = true
		}
		if len(languages) > 0 {
			return languages, true
		}
	}
	return nil, false
}

var cmakeProjectKeywords = map[string]bool{
	"VERSION":        true,
	"COMPAT_VERSION": true,
	"DESCRIPTION":    true,
	"HOMEPAGE_URL":   true,
	"SPDX_LICENSE":   true,
}

// cmakeLanguageNames maps the case-sensitive project() language names CMake
// accepts to the canonical spelling used for compiler injection.
var cmakeLanguageNames = map[string]string{
	"C": "C", "CXX": "CXX", "CUDA": "CUDA", "OBJC": "OBJC", "OBJCXX": "OBJCXX",
	"Fortran": "FORTRAN", "ASM": "ASM", "HIP": "HIP", "ISPC": "ISPC",
	"Swift": "SWIFT", "NONE": "NONE", "RC": "RC",
}

// cmakeCommandArguments returns the argument text of the first cmake command
// call with the given (case-insensitive) name.
func cmakeCommandArguments(text, name string) (string, bool) {
	lower := strings.ToLower(text)
	for offset := 0; offset < len(lower); {
		index := strings.Index(lower[offset:], name)
		if index < 0 {
			return "", false
		}
		index += offset
		offset = index + len(name)
		if index > 0 && cmakeIdentifierByte(lower[index-1]) {
			continue
		}
		open := offset
		for open < len(text) && cmakeArgumentSpace(text[open]) {
			open++
		}
		if open >= len(text) || text[open] != '(' {
			continue
		}
		if body, ok := cmakeParenthesized(text, open); ok {
			return body, true
		}
	}
	return "", false
}

func cmakeIdentifierByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

func cmakeParenthesized(text string, open int) (string, bool) {
	depth := 0
	inQuote := false
	for i := open; i < len(text); i++ {
		switch c := text[i]; {
		case inQuote:
			if c == '\\' {
				i++
			} else if c == '"' {
				inQuote = false
			}
		case c == '"':
			inQuote = true
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return text[open+1 : i], true
			}
		}
	}
	return "", false
}

func stripCMakeComments(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		inQuote := false
		for i := 0; i < len(line); i++ {
			if line[i] == '"' && (i == 0 || line[i-1] != '\\') {
				inQuote = !inQuote
				continue
			}
			if line[i] == '#' && !inQuote {
				line = line[:i]
				break
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func cmakeArguments(text string) []string {
	var args []string
	for i := 0; i < len(text); {
		for i < len(text) && cmakeArgumentSpace(text[i]) {
			i++
		}
		if i >= len(text) {
			break
		}
		if text[i] == '"' {
			i++
			start := i
			for i < len(text) && text[i] != '"' {
				if text[i] == '\\' && i+1 < len(text) {
					i++
				}
				i++
			}
			args = append(args, text[start:i])
			if i < len(text) {
				i++
			}
			continue
		}
		start := i
		for i < len(text) && !cmakeArgumentSpace(text[i]) {
			i++
		}
		args = append(args, text[start:i])
	}
	return args
}

func cmakeArgumentSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
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
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "-D") {
			definition := strings.TrimPrefix(arg, "-D")
			if definition == "" && i+1 < len(args) {
				i++
				definition = args[i]
			}
			switch cmakeDefinitionKey(definition) {
			case "CMAKE_INSTALL_PREFIX":
				return fmt.Errorf("CMAKE_INSTALL_PREFIX is managed by VMake; use SetCMakeInstallDir")
			case "CMAKE_BUILD_TYPE":
				return fmt.Errorf("CMAKE_BUILD_TYPE is managed by VMake; use SetCMakeBuildType")
			}
			continue
		}
		if strings.HasPrefix(arg, "-B") || arg == "--build" || strings.HasPrefix(arg, "--build=") || arg == "--install" || strings.HasPrefix(arg, "--install=") {
			return fmt.Errorf("CMake build directory is managed by VMake; use SetCMakeBuildDir instead of %q", arg)
		}
		if arg == "--prefix" || strings.HasPrefix(arg, "--prefix=") || arg == "--install-prefix" || strings.HasPrefix(arg, "--install-prefix=") {
			return fmt.Errorf("CMake install prefix is managed by VMake; use SetCMakeInstallDir instead of %q", arg)
		}
	}
	return nil
}

func hasCMakeGenerator(args []string) bool {
	return hasCMakeOption(args, "-G", "--preset")
}

func hasCMakeOption(args []string, options ...string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "-D" {
			i++
			continue
		}
		for _, option := range options {
			if arg == option || strings.HasPrefix(arg, option+"=") || len(option) == 2 && strings.HasPrefix(arg, option) {
				return true
			}
		}
	}
	return false
}

func cmakeDefinitions(args []string) map[string]bool {
	definitions := make(map[string]bool)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-D") {
			continue
		}
		definition := strings.TrimPrefix(arg, "-D")
		if definition == "" && i+1 < len(args) {
			i++
			definition = args[i]
		}
		definitions[cmakeDefinitionKey(definition)] = true
	}
	return definitions
}

func cmakeDefinitionKey(definition string) string {
	key, _, _ := strings.Cut(definition, "=")
	key, _, _ = strings.Cut(key, ":")
	return key
}
