package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

const (
	buildDiagnosticMessageLimit = 1000
	buildSummaryLimit           = 299
)

type buildDiagnostic struct {
	Severity      string `json:"severity"`
	Code          string `json:"code"`
	Message       string `json:"message"`
	File          string `json:"file,omitempty"`
	Line          int    `json:"line,omitempty"`
	Column        int    `json:"column,omitempty"`
	Package       string `json:"package,omitempty"`
	Target        string `json:"target,omitempty"`
	RepeatedCount int    `json:"repeatedCount,omitempty"`
}

var (
	gnuDiagnosticPattern   = regexp.MustCompile(`^(.*?):(\d+):(?:(\d+):)?\s*(fatal error|error|warning|note):\s*(.*)$`)
	gnuLinkerPattern       = regexp.MustCompile(`^(.*):(\d+):(?:\([^)]*\):)?\s*(undefined reference to .*|multiple definition of .*)$`)
	cmakeDiagnosticPattern = regexp.MustCompile(`^CMake (?:(?:Deprecation|Dev|Author) )?(Warning|Error)(?: \([^)]*\))? at (.*):(\d+)(?: \([^)]*\))?:$`)
)

func buildFailureDetails(err error) ([]buildDiagnostic, string) {
	diagnostics := []buildDiagnostic{}
	collectBuildDiagnostics(err, nil, &diagnostics)
	if err == nil {
		return diagnostics, ""
	}
	if errors.Is(err, context.Canceled) {
		return diagnostics, "build cancelled"
	}
	if diagnostic, ok := firstErrorDiagnostic(diagnostics); ok {
		return diagnostics, summarizeBuildLine(formatBuildDiagnostic(diagnostic))
	}
	return diagnostics, summarizeBuildLine(firstErrorLine(err))
}

func collectBuildDiagnostics(err error, target *build.TargetError, diagnostics *[]buildDiagnostic) {
	if err == nil {
		return
	}
	if targetErr, ok := err.(*build.TargetError); ok {
		collectBuildDiagnostics(targetErr.Err, targetErr, diagnostics)
		return
	}
	if scriptErr, ok := err.(*api.BuildScriptError); ok {
		pkgName := scriptErr.Package
		targetName := ""
		if target != nil {
			targetName = target.Target
			if pkgName == "" {
				pkgName = targetPackage(target)
			}
		}
		if parsed := parseGNUDiagnostics(scriptErr.Error()); len(parsed) > 0 {
			for i := range parsed {
				parsed[i].Code = "VMAKE_E_SCRIPT"
				parsed[i].Package = pkgName
				parsed[i].Target = targetName
			}
			*diagnostics = append(*diagnostics, parsed...)
			return
		}
		diagnostic := buildDiagnostic{Severity: "error", Code: "VMAKE_E_SCRIPT", Message: boundDiagnosticMessage(scriptErr.Error()), Package: pkgName, Target: targetName}
		*diagnostics = append(*diagnostics, diagnostic)
		return
	}
	if commandErr, ok := err.(*iexec.CommandError); ok {
		code := buildCodeForTarget(target)
		parsed := parseGNUDiagnostics(commandErr.Output)
		if len(parsed) == 0 {
			message := fallbackCommandMessage(commandErr.Output)
			if message == "" && commandErr.Err != nil {
				message = commandErr.Err.Error()
			}
			parsed = []buildDiagnostic{{Severity: "error", Code: code, Message: message}}
		}
		for i := range parsed {
			parsed[i].Code = code
			if target != nil {
				parsed[i].Package = targetPackage(target)
				parsed[i].Target = target.Target
			}
		}
		*diagnostics = append(*diagnostics, parsed...)
		return
	}
	before := len(*diagnostics)
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range multi.Unwrap() {
			collectBuildDiagnostics(child, target, diagnostics)
		}
		if target != nil && len(*diagnostics) == before {
			appendTargetFallback(err, target, diagnostics)
		}
		return
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		collectBuildDiagnostics(single.Unwrap(), target, diagnostics)
		if target != nil && len(*diagnostics) == before {
			appendTargetFallback(err, target, diagnostics)
		}
		return
	}
	if target != nil {
		appendTargetFallback(err, target, diagnostics)
	}
}

func appendTargetFallback(err error, target *build.TargetError, diagnostics *[]buildDiagnostic) {
	if errors.Is(err, context.Canceled) {
		return
	}
	pkgName := targetPackage(target)
	*diagnostics = append(*diagnostics, buildDiagnostic{
		Severity: "error",
		Code:     buildCodeForTarget(target),
		Message:  boundDiagnosticMessage(err.Error()),
		Package:  pkgName,
		Target:   target.Target,
	})
}

func parseGNUDiagnostics(output string) []buildDiagnostic {
	diagnostics := []buildDiagnostic{}
	lines := strings.Split(output, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\r")
		if match := gnuDiagnosticPattern.FindStringSubmatch(line); match != nil {
			file := strings.TrimSpace(match[1])
			if file == "" {
				continue
			}
			lineNumber, _ := strconv.Atoi(match[2])
			column, _ := strconv.Atoi(match[3])
			severity := match[4]
			if severity == "fatal error" {
				severity = "error"
			}
			diagnostics = append(diagnostics, buildDiagnostic{
				Severity: severity,
				Message:  strings.TrimSpace(match[5]),
				File:     file,
				Line:     lineNumber,
				Column:   column,
			})
			continue
		}
		if match := gnuLinkerPattern.FindStringSubmatch(line); match != nil {
			file := strings.TrimSpace(match[1])
			if file == "" {
				continue
			}
			lineNumber, _ := strconv.Atoi(match[2])
			diagnostics = append(diagnostics, buildDiagnostic{
				Severity: "error",
				Message:  strings.TrimSpace(match[3]),
				File:     file,
				Line:     lineNumber,
			})
			continue
		}
		if severity, file, lineNumber, ok := parseCMakeDiagnosticHeader(line); ok {
			message := cmakeDiagnosticMessage(lines, i, severity, file, lineNumber)
			diagnostics = append(diagnostics, buildDiagnostic{
				Severity: severity,
				Message:  message,
				File:     file,
				Line:     lineNumber,
			})
			continue
		}
		if severity, ok := parseCMakeBareHeader(line); ok {
			diagnostics = append(diagnostics, buildDiagnostic{
				Severity: severity,
				Message:  cmakeBareDiagnosticMessage(lines, i, severity),
			})
		}
	}
	return diagnostics
}

func parseCMakeBareHeader(line string) (string, bool) {
	switch strings.TrimSpace(line) {
	case "CMake Warning:":
		return "warning", true
	case "CMake Error:":
		return "error", true
	}
	return "", false
}

func cmakeDiagnosticDetails(lines []string, index int) []string {
	var parts []string
	for i := index + 1; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\r")
		if strings.TrimSpace(line) == "" {
			next := i + 1
			for next < len(lines) && strings.TrimSpace(lines[next]) == "" {
				next++
			}
			if next < len(lines) {
				nextLine := strings.TrimSuffix(lines[next], "\r")
				if (strings.HasPrefix(nextLine, " ") || strings.HasPrefix(nextLine, "\t")) && !cmakeDiagnosticTrailer(nextLine) {
					i = next - 1
					continue
				}
			}
			break
		}
		if line[0] != ' ' && line[0] != '\t' {
			break
		}
		parts = append(parts, strings.TrimSpace(line))
	}
	return parts
}

func cmakeDiagnosticTrailer(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{
		"-- ", "cmake --", "cmake build", "ninja:", "gmake", "make[", "Build files", "Configuring",
		"Generating done", "See also", "Error:",
	} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func cmakeBareDiagnosticMessage(lines []string, index int, severity string) string {
	parts := cmakeDiagnosticDetails(lines, index)
	if len(parts) == 0 {
		return "CMake " + severity
	}
	return truncateRunes(strings.Join(parts, "\n"), buildDiagnosticMessageLimit)
}

func parseCMakeDiagnosticHeader(line string) (severity, file string, lineNumber int, ok bool) {
	match := cmakeDiagnosticPattern.FindStringSubmatch(line)
	if match == nil {
		return "", "", 0, false
	}
	lineNumber, _ = strconv.Atoi(match[3])
	file = strings.TrimSpace(match[2])
	if file == "" {
		return "", "", 0, false
	}
	return strings.ToLower(match[1]), file, lineNumber, true
}

func cmakeDiagnosticMessage(lines []string, index int, severity, file string, lineNumber int) string {
	if parts := cmakeDiagnosticDetails(lines, index); len(parts) > 0 {
		return truncateRunes(strings.Join(parts, "\n"), buildDiagnosticMessageLimit)
	}
	return fmt.Sprintf("CMake %s at %s:%d", severity, file, lineNumber)
}

func fallbackCommandMessage(output string) string {
	if message := lastOutputLineContaining(output, "undefined reference", "multiple definition"); message != "" {
		return message
	}
	if message := lastOutputLineContaining(output, "error:"); message != "" {
		return message
	}
	return tailRunes(strings.TrimSpace(output), buildDiagnosticMessageLimit)
}

func lastOutputLineContaining(output string, needles ...string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.Join(strings.Fields(lines[i]), " ")
		if line == "" {
			continue
		}
		for _, needle := range needles {
			if strings.Contains(line, needle) {
				return truncateRunes(line, buildDiagnosticMessageLimit)
			}
		}
	}
	return ""
}

func buildCodeForTarget(target *build.TargetError) string {
	if target == nil {
		return "VMAKE_E_BUILD"
	}
	switch target.Stage {
	case "compile":
		return "VMAKE_E_COMPILE"
	case "link":
		return "VMAKE_E_LINK"
	default:
		return "VMAKE_E_BUILD"
	}
}

func targetPackage(target *build.TargetError) string {
	pkg, _, ok := strings.Cut(target.Target, ":")
	if !ok {
		return target.Target
	}
	return pkg
}

func firstErrorDiagnostic(diagnostics []buildDiagnostic) (buildDiagnostic, bool) {
	var fallback *buildDiagnostic
	for i := range diagnostics {
		if diagnostics[i].Severity != "error" {
			continue
		}
		if diagnostics[i].File != "" {
			return diagnostics[i], true
		}
		if fallback == nil {
			fallback = &diagnostics[i]
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	if len(diagnostics) > 0 {
		return diagnostics[0], true
	}
	return buildDiagnostic{}, false
}

func formatBuildDiagnostic(diagnostic buildDiagnostic) string {
	if diagnostic.File == "" {
		return diagnostic.Message
	}
	location := diagnostic.File
	if diagnostic.Line > 0 {
		location += ":" + strconv.Itoa(diagnostic.Line)
		if diagnostic.Column > 0 {
			location += ":" + strconv.Itoa(diagnostic.Column)
		}
	}
	if diagnostic.Message == "" {
		return location
	}
	return location + ": " + diagnostic.Message
}

func firstErrorLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}

func summarizeBuildLine(line string) string {
	return truncateRunes(strings.Join(strings.Fields(line), " "), buildSummaryLimit)
}

func boundDiagnosticMessage(text string) string {
	const marker = "...[truncated]\n"
	if len([]rune(text)) <= buildDiagnosticMessageLimit {
		return text
	}
	return marker + tailRunes(text, buildDiagnosticMessageLimit-len([]rune(marker)))
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func tailRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[len(runes)-limit:])
}
