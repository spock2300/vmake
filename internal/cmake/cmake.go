package cmake

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/pkg/toolchain"
)

type Settings struct {
	SourceDir    string
	BuildRoot    string
	BuildDir     string
	InstallDir   string
	BuildType    string
	HostOS       string
	TargetOS     string
	TargetTriple string
	Toolchain    *toolchain.Toolchain
	GlobalFlags  []string
	DryRun       bool
}

func ConfigureArgs(s Settings, extra ...string) ([]string, error) {
	if err := ValidateArgs(extra); err != nil {
		return nil, err
	}
	if s.Toolchain == nil {
		return nil, fmt.Errorf("toolchain is not configured")
	}
	args := []string{
		"-S", filepath.ToSlash(s.SourceDir),
		"-B", filepath.ToSlash(s.BuildDir),
		"-DCMAKE_INSTALL_PREFIX=" + filepath.ToSlash(s.InstallDir),
	}
	languages, languageList := ProjectLanguages(s.SourceDir)
	// Compiler and compiler-target arguments follow the project's declared
	// languages so CMake does not report vmake-provided variables it never
	// consumes; binutils stay unconditional. Without a parseable language list
	// every compiler is injected.
	for _, tool := range []struct{ key, language, name string }{
		{"CMAKE_C_COMPILER", "C", s.Toolchain.Tools.CC},
		{"CMAKE_CXX_COMPILER", "CXX", s.Toolchain.Tools.CXX},
		{"CMAKE_ASM_COMPILER", "ASM", s.Toolchain.Tools.CC},
		{"CMAKE_AR", "", s.Toolchain.Tools.AR},
		{"CMAKE_RANLIB", "", s.Toolchain.Tools.RANLIB},
		{"CMAKE_STRIP", "", s.Toolchain.Tools.STRIP},
		{"CMAKE_OBJCOPY", "", s.Toolchain.Tools.OBJCOPY},
		{"CMAKE_NM", "", s.Toolchain.Tools.NM},
	} {
		if tool.name == "" {
			continue
		}
		if languageList && tool.language != "" && !languages[tool.language] {
			continue
		}
		path, err := toolchain.ResolveToolPath(tool.name, s.Toolchain.InstallPath)
		if err != nil {
			return nil, fmt.Errorf("resolve %s %q: %w", tool.key, tool.name, err)
		}
		args = append(args, "-D"+tool.key+"="+filepath.ToSlash(path))
	}
	args = append(args, "-DCMAKE_BUILD_TYPE="+s.BuildType)
	if s.TargetTriple != "" || s.TargetOS != s.HostOS {
		systemNames := map[string]string{
			"none": "Generic", "linux": "Linux", "windows": "Windows",
			"darwin": "Darwin", "freebsd": "FreeBSD", "openbsd": "OpenBSD",
			"netbsd": "NetBSD", "android": "Android",
		}
		systemName, ok := systemNames[s.TargetOS]
		if !ok {
			return nil, fmt.Errorf("unsupported CMake target OS %q", s.TargetOS)
		}
		args = append(args, "-DCMAKE_SYSTEM_NAME="+systemName)
	}
	if s.TargetOS == "none" {
		args = append(args,
			"-DCMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY",
			"-DCMAKE_FIND_ROOT_PATH_MODE_PROGRAM=NEVER",
			"-DCMAKE_FIND_ROOT_PATH_MODE_LIBRARY=ONLY",
			"-DCMAKE_FIND_ROOT_PATH_MODE_INCLUDE=ONLY",
		)
	}
	if s.TargetTriple != "" {
		if !languageList || languages["C"] {
			args = append(args, "-DCMAKE_C_COMPILER_TARGET="+s.TargetTriple)
		}
		if !languageList || languages["CXX"] {
			args = append(args, "-DCMAKE_CXX_COMPILER_TARGET="+s.TargetTriple)
		}
		if !languageList || languages["ASM"] {
			args = append(args, "-DCMAKE_ASM_COMPILER_TARGET="+s.TargetTriple)
		}
	}
	if s.HostOS == "windows" && !HasGenerator(extra) && os.Getenv("CMAKE_GENERATOR") == "" {
		args = append(args, "-G", "Ninja")
	}
	definitions := Definitions(extra)
	for _, arg := range s.GlobalFlags {
		if languageList {
			if strings.HasPrefix(arg, "-DCMAKE_C_FLAGS=") && !languages["C"] {
				continue
			}
			if strings.HasPrefix(arg, "-DCMAKE_CXX_FLAGS=") && !languages["CXX"] {
				continue
			}
		}
		if !definitions[DefinitionKey(strings.TrimPrefix(arg, "-D"))] {
			args = append(args, arg)
		}
	}
	args = append(args, extra...)
	return args, nil
}

func ConfigureDir(s Settings, extra []string) string {
	if HasOption(extra, "--preset") {
		return s.SourceDir
	}
	return s.BuildRoot
}

// ProjectLanguages returns the languages declared by the source tree's
// project() command, parsing both the LANGUAGES form and the legacy
// project(<name> <language>...) form. ok is false when CMakeLists.txt is
// missing, has no project() call, or no language list is parseable; callers
// then keep the unconditional compiler injection.
func ProjectLanguages(srcDir string) (map[string]bool, bool) {
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

func ValidateArgs(args []string) error {
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
			switch DefinitionKey(definition) {
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

func HasGenerator(args []string) bool {
	return HasOption(args, "-G", "--preset")
}

func HasOption(args []string, options ...string) bool {
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

func Definitions(args []string) map[string]bool {
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
		definitions[DefinitionKey(definition)] = true
	}
	return definitions
}

func DefinitionKey(definition string) string {
	key, _, _ := strings.Cut(definition, "=")
	key, _, _ = strings.Cut(key, ":")
	return key
}
