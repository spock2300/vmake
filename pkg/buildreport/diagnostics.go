package buildreport

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
	DiagnosticMessageLimit = 1000
	SummaryLimit           = 299
)

type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Package  string `json:"package,omitempty"`
	Target   string `json:"target,omitempty"`
}

var (
	gnuDiagnosticPattern = regexp.MustCompile(`^(.*?):(\d+):(?:(\d+):)?\s*(fatal error|error|warning|note):\s*(.*)$`)
	gnuLinkerPattern     = regexp.MustCompile(`^(.*):(\d+):(?:\([^)]*\):)?\s*(undefined reference to .*|multiple definition of .*)$`)
)

func FailureDetails(err error) ([]Diagnostic, string) {
	diagnostics := []Diagnostic{}
	collectDiagnostics(err, nil, &diagnostics)
	if err == nil {
		return diagnostics, ""
	}
	if errors.Is(err, context.Canceled) {
		return diagnostics, "build cancelled"
	}
	if diagnostic, ok := firstErrorDiagnostic(diagnostics); ok {
		return diagnostics, SummarizeLine(FormatDiagnostic(diagnostic))
	}
	return diagnostics, SummarizeLine(FirstErrorLine(err))
}

func collectDiagnostics(err error, target *build.TargetError, diagnostics *[]Diagnostic) {
	if err == nil {
		return
	}
	if targetErr, ok := err.(*build.TargetError); ok {
		collectDiagnostics(targetErr.Err, targetErr, diagnostics)
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
		if parsed := ParseGNUDiagnostics(scriptErr.Error()); len(parsed) > 0 {
			for i := range parsed {
				parsed[i].Code = "VMAKE_E_SCRIPT"
				parsed[i].Package = pkgName
				parsed[i].Target = targetName
			}
			*diagnostics = append(*diagnostics, parsed...)
			return
		}
		diagnostic := Diagnostic{Severity: "error", Code: "VMAKE_E_SCRIPT", Message: BoundDiagnosticMessage(scriptErr.Error()), Package: pkgName, Target: targetName}
		*diagnostics = append(*diagnostics, diagnostic)
		return
	}
	if commandErr, ok := err.(*iexec.CommandError); ok {
		code := buildCodeForTarget(target)
		parsed := ParseGNUDiagnostics(commandErr.Output)
		if len(parsed) == 0 {
			message := fallbackCommandMessage(commandErr.Output)
			if message == "" && commandErr.Err != nil {
				message = commandErr.Err.Error()
			}
			parsed = []Diagnostic{{Severity: "error", Code: code, Message: message}}
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
			collectDiagnostics(child, target, diagnostics)
		}
		if target != nil && len(*diagnostics) == before {
			appendTargetFallback(err, target, diagnostics)
		}
		return
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		collectDiagnostics(single.Unwrap(), target, diagnostics)
		if target != nil && len(*diagnostics) == before {
			appendTargetFallback(err, target, diagnostics)
		}
		return
	}
	if target != nil {
		appendTargetFallback(err, target, diagnostics)
	}
}

func appendTargetFallback(err error, target *build.TargetError, diagnostics *[]Diagnostic) {
	if errors.Is(err, context.Canceled) {
		return
	}
	pkgName := targetPackage(target)
	*diagnostics = append(*diagnostics, Diagnostic{
		Severity: "error",
		Code:     buildCodeForTarget(target),
		Message:  BoundDiagnosticMessage(err.Error()),
		Package:  pkgName,
		Target:   target.Target,
	})
}

func ParseGNUDiagnostics(output string) []Diagnostic {
	diagnostics := []Diagnostic{}
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
			diagnostics = append(diagnostics, Diagnostic{
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
			diagnostics = append(diagnostics, Diagnostic{
				Severity: "error",
				Message:  strings.TrimSpace(match[3]),
				File:     file,
				Line:     lineNumber,
			})
		}
	}
	return diagnostics
}

func fallbackCommandMessage(output string) string {
	if message := lastOutputLineContaining(output, "undefined reference", "multiple definition"); message != "" {
		return message
	}
	if message := lastOutputLineContaining(output, "error:"); message != "" {
		return message
	}
	return tailRunes(strings.TrimSpace(output), DiagnosticMessageLimit)
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
				return truncateRunes(line, DiagnosticMessageLimit)
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

func firstErrorDiagnostic(diagnostics []Diagnostic) (Diagnostic, bool) {
	var fallback *Diagnostic
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
	return Diagnostic{}, false
}

func FormatDiagnostic(diagnostic Diagnostic) string {
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

func FirstErrorLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}

func SummarizeLine(line string) string {
	return truncateRunes(strings.Join(strings.Fields(line), " "), SummaryLimit)
}

func BoundDiagnosticMessage(text string) string {
	const marker = "...[truncated]\n"
	if len([]rune(text)) <= DiagnosticMessageLimit {
		return text
	}
	return marker + tailRunes(text, DiagnosticMessageLimit-len([]rune(marker)))
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
