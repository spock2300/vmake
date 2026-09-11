package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/buildscript"
	vlog "github.com/spock2300/vmake/pkg/log"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose platform prerequisites and build.go patterns",
	Long: `Reports the status of the platform prerequisites vmake depends on
(symbolic links, the Git for Windows userland, make, the C toolchain) and
scans build.go files in the current project for patterns that currently work
but may become errors in future vmake versions:

  - [autoWire] Package uses OnRequire/AddRequires but its targets don't call
    AddDeps. vmake currently auto-wires the dependency edges as a convenience,
    but this implicit behavior conflicts with the No-Fallbacks principle and
    may be removed. Add explicit AddDeps("pkg:target") to each target.

  - [noRoot] No package declares SetRoot(true). vmake currently uses heuristics
    to pick build roots, but these may produce surprising results in projects
    with mixed apps/libraries. Adding SetRoot(true) to the entry-point package
    makes the build intent explicit.`,
	Run: func(cmd *cobra.Command, args []string) {
		runDoctor()
	},
}

func init() {
	RootCmd.AddCommand(doctorCmd)
}

type doctorFinding struct {
	Severity string
	File     string
	Category string
	Message  string
}

func runDoctor() {
	workDir, err := os.Getwd()
	if err != nil {
		vlog.Fatal("getwd: %v", err)
	}

	findings := checkPlatform()

	sources, err := buildscript.Scan(workDir)
	if err != nil {
		vlog.Fatal("scan: %v", err)
	}
	if len(sources) == 0 {
		reportDoctorFindings(workDir, findings)
		vlog.Fatal("no build.go files found")
	}

	rootCount := 0
	for _, src := range sources {
		findings = append(findings, checkAutoWire(src)...)
		findings = append(findings, checkDeprecated(src)...)
		if hasSetRoot(src.Path) {
			rootCount++
		}
	}

	if rootCount == 0 {
		findings = append(findings, doctorFinding{
			Severity: "warn",
			Category: "noRoot",
			Message:  "no package declares SetRoot(true); build roots are inferred heuristically (see AGENTS.md 'computeReachable')",
		})
	}
	if rootCount > 1 {
		findings = append(findings, doctorFinding{
			Severity: "error",
			Category: "noRoot",
			Message:  fmt.Sprintf("%d packages declare SetRoot(true); only one is allowed", rootCount),
		})
	}

	reportDoctorFindings(workDir, findings)
}

func reportDoctorFindings(workDir string, findings []doctorFinding) {
	errors := 0
	warns := 0
	for _, f := range findings {
		prefix := ""
		if f.File != "" {
			rel, err := filepath.Rel(workDir, f.File)
			if err == nil {
				prefix = rel + ": "
			} else {
				prefix = f.File + ": "
			}
		}
		vlog.Info("[%s] %s%s (%s)", f.Severity, prefix, f.Message, f.Category)

		switch f.Severity {
		case "error":
			errors++
		case "warn":
			warns++
		}
	}

	vlog.Info("")
	if errors == 0 && warns == 0 {
		vlog.Info("OK: no issues")
		return
	}
	vlog.Info("Summary: %d error(s), %d warning(s)", errors, warns)
	if errors > 0 {
		os.Exit(1)
	}
}

func checkAutoWire(src buildscript.Source) []doctorFinding {
	data, err := os.ReadFile(src.Path)
	if err != nil {
		return nil
	}
	content := string(data)

	if !strings.Contains(content, "AddRequires") {
		return nil
	}
	if strings.Contains(content, "AddDeps") {
		return nil
	}

	return []doctorFinding{{
		Severity: "warn",
		File:     src.Path,
		Category: "autoWire",
		Message:  "package uses AddRequires but targets have no AddDeps; relies on autoWireRequireDeps fallback",
	}}
}

func hasSetRoot(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "SetRoot(true)")
}

var deprecatedAPIs = []struct {
	old, new, category string
}{
	{"SetExcludeLibs(", "AddExcludeLibs(", "SetExcludeLibs"},
	{"PatchKConfig(", "SetKConfigPatches(", "PatchKConfig"},
	{"SetSelectedPreset(", "SelectPreset(", "SetSelectedPreset"},
	{"IfNot(", "When(", "IfNot"},
	{"ctx.Equal(", "ctx.When(", "Equal"},
	{"os.Chdir(", "absolute paths or p.RunIn", "os.Chdir"},
	{"ioutil.", "os.", "ioutil"},
}

func checkDeprecated(src buildscript.Source) []doctorFinding {
	data, err := os.ReadFile(src.Path)
	if err != nil {
		return nil
	}
	content := string(data)

	var findings []doctorFinding
	for _, d := range deprecatedAPIs {
		if strings.Contains(content, d.old) {
			findings = append(findings, doctorFinding{
				Severity: "warn",
				File:     src.Path,
				Category: d.category,
				Message:  fmt.Sprintf("use %s instead of deprecated %s", d.new, strings.TrimSuffix(d.old, "(")),
			})
		}
	}
	return findings
}
