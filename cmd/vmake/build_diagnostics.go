package main

import (
	"context"
	"errors"
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
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Package  string `json:"package,omitempty"`
	Target   string `json:"target,omitempty"`
}

var gnuDiagnosticPattern = regexp.MustCompile(`^(.*?):(\d+):(?:(\d+):)?\s*(fatal error|error|warning|note):\s*(.*)$`)

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
		diagnostic := buildDiagnostic{Severity: "error", Code: "VMAKE_E_SCRIPT", Message: scriptErr.Error(), Package: scriptErr.Package}
		if target != nil {
			diagnostic.Target = target.Target
			if diagnostic.Package == "" {
				diagnostic.Package = targetPackage(target)
			}
		}
		*diagnostics = append(*diagnostics, diagnostic)
		return
	}
	if commandErr, ok := err.(*iexec.CommandError); ok {
		code := buildCodeForTarget(target)
		parsed := parseGNUDiagnostics(commandErr.Output)
		if len(parsed) == 0 {
			message := tailRunes(strings.TrimSpace(commandErr.Output), buildDiagnosticMessageLimit)
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
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range multi.Unwrap() {
			collectBuildDiagnostics(child, target, diagnostics)
		}
		return
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		collectBuildDiagnostics(single.Unwrap(), target, diagnostics)
	}
}

func parseGNUDiagnostics(output string) []buildDiagnostic {
	diagnostics := []buildDiagnostic{}
	for _, line := range strings.Split(output, "\n") {
		match := gnuDiagnosticPattern.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if match == nil {
			continue
		}
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
	}
	return diagnostics
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
