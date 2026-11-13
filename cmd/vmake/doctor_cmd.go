package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/gosrc"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
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

func init() {
	RootCmd.AddCommand(doctorCmd)
	addConfigFlag(doctorCmd)
	doctorCmd.Flags().StringVar(&toolchainFlag, "toolchain", "", "toolchain to diagnose (default: active config, chip PreferredToolchain, or host)")
	doctorCmd.RegisterFlagCompletionFunc("toolchain", completeToolchain)
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
		vlog.Fatal("getwd: %v", err)
	}

	workDir := startDir
	if projectDir := findProjectDirSoft(); projectDir != "" {
		workDir = projectDir
	}
	cfg, _, err := config.LoadProjectWithOverride(workDir, configFlag)
	if err != nil {
		vlog.Fatal("load project config: %v", err)
	}
	nested := checkNestedProjectMarker(startDir, workDir)
	sources, err := buildscript.Scan(workDir)
	if err != nil {
		vlog.Fatal("scan: %v", err)
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
	findings = append(findings, nested...)

	if len(sources) == 0 {
		reportDoctorFindings(workDir, findings)
		vlog.Fatal("no build.go files found")
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

func resolveDoctorPreferredToolchain(workDir string, cfg *config.ConfigFile) string {
	pins, err := config.LoadProjectPins(workDir)
	if err != nil {
		return ""
	}
	local, err := config.LoadLocal(workDir)
	if err != nil {
		return ""
	}
	lockPath := filepath.Join(workDir, ".vmake", "vmake.lock")
	lock := lockfile.New()
	if loaded, err := lockfile.Load(lockPath); err == nil {
		lock = loaded
	}
	paths := &pipeline.Paths{
		ProjectDir: workDir,
		DepsDir:    filepath.Join(workDir, ".vmake_deps"),
		CacheDir:   getCacheDir(),
		ReposDir:   getReposDir(),
		LockPath:   lockPath,
	}
	prevLevel := vlog.GetLevel()
	vlog.SetLevel(vlog.Quiet)
	iexec.SetQuiet(true)
	scriptfs.SetReadOnly(true)
	restoreOut := silenceStdout()
	restoreErr := silenceStderr()
	defer func() {
		restoreOut()
		restoreErr()
		scriptfs.SetReadOnly(false)
		vlog.SetLevel(prevLevel)
		iexec.SetQuiet(vlog.IsQuiet())
	}()

	ctx := pipeline.NewContext(pipeline.ResolveParams{
		WorkDir:      workDir,
		Config:       cfg,
		Lock:         lock,
		LockPath:     lockPath,
		Pins:         pins,
		SourceLinks:  local.Links,
		TrustChecker: remoteScriptTrustChecker,
		Paths:        paths,
	})
	ctx.Context = context.Background()
	ctx.LockPolicy = resolver.LockPolicy{Offline: true, DryRun: true}
	defer closeProject(ctx)

	if err := pipeline.Require(ctx); err != nil {
		return ""
	}
	if err := pipeline.Configure(ctx); err != nil {
		return ""
	}
	pipeline.ResolveActiveChipForDisplay(ctx)
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
