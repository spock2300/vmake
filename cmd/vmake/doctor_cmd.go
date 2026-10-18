package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/gosrc"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/toolchain"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose platform prerequisites and build.go patterns",
	Long: `Reports the status of the platform prerequisites vmake depends on
(symbolic links, the Git for Windows userland, make, the C toolchain) and
scans build.go files in the current project for patterns that currently work
but may become errors in future vmake versions:

  - [autoWire] Package uses OnRequire/AddRequires but its targets don't call
    AddDeps. AddRequires only resolves and fetches the package; it does not
    create target build edges. Add explicit AddDeps("pkg:target") to each
    target that links the dependency.

  - [noRoot] No package declares SetRoot(true). vmake currently uses heuristics
    to pick build roots, but these may produce surprising results in projects
    with mixed apps/libraries. Adding SetRoot(true) to the entry-point package
    makes the build intent explicit.

With --json, prints one machine-readable document (schemaVersion 1) on stdout
and suppresses the human lines: {"schemaVersion":1,"status":"ok"|"warnings"|
"error","root":"...","findings":[{"severity":"warn"|"error","category":"...",
"file":"...","message":"..."}]}. Only warnings and errors are listed, so a
clean project has an empty findings array. The exit status is 0 for ok/warnings
and 1 when any error finding exists.`,
	Run: func(cmd *cobra.Command, args []string) {
		runDoctor()
	},
}

var doctorJSON bool

func init() {
	RootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().StringVar(&toolchainFlag, "toolchain", "", "toolchain to diagnose (default: project selection)")
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "emit one JSON diagnostic document on stdout")
	doctorCmd.RegisterFlagCompletionFunc("toolchain", completeToolchain)
}

type doctorFinding struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	File     string `json:"file"`
	Message  string `json:"message"`
}

type doctorJSONDocument struct {
	SchemaVersion int             `json:"schemaVersion"`
	Status        string          `json:"status"`
	Root          string          `json:"root"`
	Findings      []doctorFinding `json:"findings"`
}

func runDoctor() {
	startDir, err := os.Getwd()
	if err != nil {
		vlog.Fatal("getwd: %v", err)
	}

	workDir := startDir
	if projectDir := findProjectDirSoft(); projectDir != "" {
		workDir = projectDir
	}
	cfg, _, err := config.LoadProject(workDir)
	if err != nil {
		if doctorJSON {
			reportDoctorJSON(workDir, nil, &doctorFinding{Severity: "error", Category: "config", Message: err.Error()})
		}
		vlog.Fatal("load project config: %v", err)
	}
	tcName := pipeline.ResolveToolchainName(cfg, toolchainFlag)
	tc, tcErr := toolchain.GetManager().GetToolchain(tcName)
	findings := checkPlatform(tc)
	if tcErr != nil {
		findings = append(findings, doctorFinding{Severity: "error", Category: "toolchain", Message: tcErr.Error()})
	}
	findings = append(findings, checkNestedProjectMarker(startDir, workDir)...)

	sources, err := buildscript.Scan(workDir)
	if err != nil {
		if doctorJSON {
			reportDoctorJSON(workDir, findings, &doctorFinding{Severity: "error", Category: "scan", Message: err.Error()})
		}
		vlog.Fatal("scan: %v", err)
	}
	if len(sources) == 0 {
		if doctorJSON {
			reportDoctorJSON(workDir, findings, &doctorFinding{Severity: "error", Category: "noBuildGo", Message: fmt.Sprintf("no build.go files found in %s", workDir)})
		}
		reportDoctorFindings(workDir, findings)
		vlog.Fatal("no build.go files found")
	}

	findings = append(findings, checkStaleProjectMarkers(workDir, sources)...)

	rootCount := 0
	for _, src := range sources {
		findings = append(findings, checkAutoWire(src)...)
		findings = append(findings, checkDeprecated(src)...)
		if hasSetRoot(src) {
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

	if doctorJSON {
		reportDoctorJSON(workDir, findings, nil)
	}
	reportDoctorFindings(workDir, findings)
}

// reportDoctorJSON prints the machine-readable doctor result and exits. Only
// warning and error findings are listed, sorted so equal runs produce the same
// document. A failure that prevents a normal diagnosis is reported as an error
// finding.
func reportDoctorJSON(workDir string, findings []doctorFinding, failure *doctorFinding) {
	if failure != nil {
		findings = append(findings, *failure)
	}
	reported := make([]doctorFinding, 0, len(findings))
	for _, f := range findings {
		if f.Severity != "warn" && f.Severity != "error" {
			continue
		}
		f.File = doctorRelativeFile(workDir, f.File)
		reported = append(reported, f)
	}
	sort.Slice(reported, func(i, j int) bool {
		if reported[i].Severity != reported[j].Severity {
			return reported[i].Severity == "error"
		}
		if reported[i].Category != reported[j].Category {
			return reported[i].Category < reported[j].Category
		}
		if reported[i].File != reported[j].File {
			return reported[i].File < reported[j].File
		}
		return reported[i].Message < reported[j].Message
	})

	status := "ok"
	code := 0
	for _, f := range reported {
		if f.Severity == "error" {
			status = "error"
			code = 1
			break
		}
		status = "warnings"
	}
	document := doctorJSONDocument{SchemaVersion: 1, Status: status, Root: workDir, Findings: reported}
	if err := encodeJSONDocument(os.Stdout, document); err != nil {
		vlog.Error("Error: %v", err)
		code = 1
	}
	os.Exit(code)
}

// doctorRelativeFile reports paths under the project root relative to it, like
// the human output, and leaves external paths absolute.
func doctorRelativeFile(root, file string) string {
	if file == "" {
		return ""
	}
	rel, err := filepath.Rel(root, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return file
	}
	return rel
}

// checkNestedProjectMarker reports a package-local .vmake marker that wins
// root detection over an enclosing project. It only fires when the invocation
// directory itself resolved as the project root; invocations from a package
// subdirectory or with -C pointing elsewhere are not reported here.
func checkNestedProjectMarker(startDir, workDir string) []doctorFinding {
	if filepath.Clean(startDir) != filepath.Clean(workDir) || !hasProjectConfig(workDir) {
		return nil
	}
	enclosing, ok := findEnclosingProjectRoot(workDir)
	if !ok || filepath.Clean(enclosing) == filepath.Clean(workDir) {
		return nil
	}
	marker := filepath.Join(workDir, ".vmake")
	return []doctorFinding{{
		Severity: "warn",
		File:     marker,
		Category: "nestedProjectMarker",
		Message:  fmt.Sprintf("current directory %s has its own .vmake marker, so vmake selected it as the project root instead of the enclosing project root %s; run vmake -C %s ... or remove %s; this nested marker shadows the project root", workDir, enclosing, enclosing, marker),
	}}
}

func checkStaleProjectMarkers(workDir string, sources []buildscript.Source) []doctorFinding {
	var findings []doctorFinding
	for _, src := range sources {
		if src.Dir == "" || src.Dir == workDir {
			continue
		}
		marker := filepath.Join(src.Dir, ".vmake")
		if info, err := os.Stat(marker); err != nil || !info.IsDir() {
			continue
		}
		findings = append(findings, doctorFinding{
			Severity: "warn",
			File:     marker,
			Category: "staleProjectMarker",
			Message:  fmt.Sprintf("package %s contains a .vmake directory; running vmake from that directory treats it as the project root. Remove %s if this is a package of the current project.", src.Name, marker),
		})
	}
	return findings
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
	if !packageSourceContains(src, "AddRequires") {
		return nil
	}
	if packageSourceContains(src, "AddDeps") {
		return nil
	}

	return []doctorFinding{{
		Severity: "warn",
		File:     src.Path,
		Category: "autoWire",
		Message:  "package uses AddRequires but targets have no AddDeps; AddRequires does not create target build edges",
	}}
}

func packageSourceContains(src buildscript.Source, needle string) bool {
	if src.Dir != "" {
		files, err := gosrc.ListGoFiles(src.Dir)
		if err == nil {
			for _, file := range files {
				if fileContains(file, needle) {
					return true
				}
			}
			return false
		}
	}
	return fileContains(src.Path, needle)
}

func fileContains(path, needle string) bool {
	data, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(data), needle)
}

func hasSetRoot(src buildscript.Source) bool {
	return packageSourceContains(src, "SetRoot(true)")
}

var deprecatedAPIs = []struct {
	old, new, category string
}{
	{"CrossTarget(", "TargetTriple(", "CrossTarget"},
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
