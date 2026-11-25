package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/gosrc"
	quietpkg "github.com/spock2300/vmake/internal/quiet"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
	"github.com/spock2300/vmake/pkg/version"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose platform prerequisites and build.go patterns",
	Long: `Reports the status of the platform prerequisites vmake depends on
(symbolic links, the Git for Windows userland, make, the C toolchain) and
scans build.go files in the current project for patterns that currently work
but may become errors in future vmake versions:

  - [autoWire] Package uses OnRequire/AddRequires but its targets don't call
    AddDeps or UseRequires. AddRequires only resolves and fetches the package;
    it does not create target build edges. Add explicit AddDeps("pkg:target")
    or UseRequires() to each target that links the dependency.

  - [noRoot] No package declares SetRoot(true). vmake currently uses heuristics
    to pick build roots, but these may produce surprising results in projects
    with mixed apps/libraries. Adding SetRoot(true) to the entry-point package
    makes the build intent explicit.`,
	Run: func(cmd *cobra.Command, args []string) {
		runDoctor()
	},
}

type doctorFinding struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	File     string `json:"file"`
	Message  string `json:"message"`
}

func runDoctor() {
	startDir, err := os.Getwd()
	if err != nil {
		fatalMsg("getwd: %v", err)
	}

	workDir := startDir
	if projectDir := findProjectDirSoft(); projectDir != "" {
		workDir = projectDir
	}
	cfg, _, err := config.LoadProjectWithOverride(workDir, configFlag)
	if err != nil {
		fatalMsg("load project config: %v", err)
	}
	nested := checkNestedProjectMarker(startDir, workDir)
	sources, err := buildscript.Scan(workDir)
	if err != nil {
		fatalMsg("scan: %v", err)
	}
	fallbackTC := ""
	if toolchainFlag == "" && (cfg.Global == nil || cfg.Global.Toolchain == "") && len(nested) == 0 && len(sources) > 0 {
		fallbackTC = resolveDoctorPreferredToolchain(workDir, cfg)
	}
	tcName := pipeline.ResolveToolchainName(cfg, toolchainFlag, fallbackTC)
	tc, tcErr := toolchain.GetManager().GetToolchain(tcName)
	findings := checkPlatform(tc)
	if tcErr != nil {
		findings = append(findings, doctorFinding{Severity: "error", Category: "toolchain", Message: tcErr.Error()})
	}
	findings = append(findings, checkInstalledSkill(getSkillPath())...)
	findings = append(findings, nested...)

	if len(sources) == 0 {
		reportDoctorFindings(workDir, findings)
		fatalMsg("no build.go files found")
	}

	rootCount := 0
	for _, src := range sources {
		findings = append(findings, checkAutoWire(src)...)
		if hasSetRoot(src) {
			rootCount++
		}
	}

	if rootCount == 0 {
		findings = append(findings, doctorFinding{
			Severity: "warn",
			Category: "noRoot",
			Message:  "no package declares SetRoot(true); build roots are inferred heuristically (see AGENTS.md 'ComputeNeeded')",
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

func checkNestedProjectMarker(startDir, workDir string) []doctorFinding {
	if projectOverride != "" || !hasProjectConfig(workDir) {
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

func reportDoctorFindings(workDir string, findings []doctorFinding) {
	errors := 0
	warns := 0
	for _, f := range findings {
		prefix := ""
		if f.File != "" {
			rel, err := filepath.Rel(workDir, f.File)
			if err == nil && !strings.HasPrefix(rel, "..") {
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
		exitWithCode(1)
	}
}

func checkInstalledSkill(dir string) []doctorFinding {
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		return nil
	}
	m, err := readSkillManifest(dir)
	if err != nil {
		message := "installed vmake skill has no version record; run vmake skill install"
		if !os.IsNotExist(err) {
			message = fmt.Sprintf("cannot read the installed vmake skill version: %v; run vmake skill install", err)
		}
		return []doctorFinding{{Severity: "error", Category: "skill", File: dir, Message: message}}
	}
	if m.SchemaVersion != 1 || m.SkillDigest == "" {
		return []doctorFinding{{
			Severity: "error",
			Category: "skill",
			File:     dir,
			Message:  "installed vmake skill has an invalid version record; run vmake skill install",
		}}
	}
	digest, err := embeddedSkillDigest()
	if err != nil {
		return []doctorFinding{{
			Severity: "error",
			Category: "skill",
			File:     dir,
			Message:  fmt.Sprintf("cannot compute the embedded skill digest: %v", err),
		}}
	}
	if m.SkillDigest != digest {
		message := fmt.Sprintf("installed vmake skill does not match this vmake binary (installed by %s %s, running %s %s); run vmake skill install",
			m.Version, m.Commit, version.Version, version.GitCommit)
		if m.Version == version.Version && m.Commit == version.GitCommit {
			message = "installed vmake skill content does not match this vmake binary; run vmake skill install"
		}
		return []doctorFinding{{
			Severity: "error",
			Category: "skill",
			File:     dir,
			Message:  message,
		}}
	}
	return []doctorFinding{{
		Severity: "ok",
		Category: "skill",
		Message:  "installed vmake skill matches " + version.Version,
	}}
}

func checkAutoWire(src buildscript.Source) []doctorFinding {
	if !packageSourceContains(src, "AddRequires") {
		return nil
	}
	if packageSourceContains(src, "AddDeps") || packageSourceContains(src, "UseRequires") {
		return nil
	}

	return []doctorFinding{{
		Severity: "warn",
		File:     src.Path,
		Category: "autoWire",
		Message:  "package uses AddRequires but targets have no AddDeps or UseRequires; AddRequires does not create target build edges",
	}}
}

func resolveDoctorPreferredToolchain(workDir string, _ *config.ConfigFile) string {
	restoreScope := quietpkg.ProbeScope(true, true)
	restoreOut := quietpkg.SilenceStdout()
	restoreErr := quietpkg.SilenceStderr()
	defer func() {
		restoreOut()
		restoreErr()
		restoreScope()
	}()

	ctx, err := openProject(projectOptions{
		root:           workDir,
		policy:         &resolver.LockPolicy{Offline: true, DryRun: true},
		skipGitignore:  true,
		noStorageLocks: true,
	})
	if err != nil {
		return ""
	}
	defer closeProject(ctx)
	return ctx.PreferredToolchain
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
