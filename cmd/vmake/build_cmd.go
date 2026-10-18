package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the project",
	Long: `Compile and link all targets defined in build.go files.

With --check (or --plan), resolve the project and report pending
compile/link/callback work without executing any build, generation, post-link
or install action.`,
	RunE: runBuild,
}

func init() {
	RootCmd.AddCommand(buildCmd)
	addInstallFlags(buildCmd)
	addBuildFlags(buildCmd)
	addCheckFlags(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	commandStorageLocks()
	if checkFlag || planFlag {
		return runCheck()
	}
	return withBuildReport(func(report *buildReport) error {
		execution := &RuntimeContext{}
		return withResolvedProject(execution, func(ctx *RuntimeContext) error {
			_, err := runReportedBuild(ctx, report, BuildOptions{IncludeTests: testsFlag, Jobs: jobsFlag, KeepGoing: keepGoingFlag, Explain: explainFlag})
			return err
		})
	})
}

func runCheck() error {
	if installFlag {
		return &usageError{err: fmt.Errorf("--check cannot be combined with --install; run the build without --check to install")}
	}
	if testsFlag {
		return &usageError{err: fmt.Errorf("--check cannot be combined with --tests; test targets are not planned")}
	}
	stdout := os.Stdout
	restore := redirectCheckStdout(stdout)
	defer restore()
	result, runErr := resolveCheck()
	restore()
	return emitCheckResult(stdout, result, runErr)
}

func resolveCheck() (*BuildResult, error) {
	execution := &RuntimeContext{}
	var result *BuildResult
	err := withResolvedProject(execution, func(ctx *RuntimeContext) error {
		var runErr error
		result, runErr = runBuildPhase(ctx, BuildOptions{Check: true, Jobs: jobsFlag, KeepGoing: keepGoingFlag, Explain: explainFlag})
		return runErr
	})
	return result, err
}

// redirectCheckStdout keeps the human stdout stream to one plan line by sending
// buildscript/progress output to stderr (or suppressing it under -q). The JSON
// envelope is written to the original stdout afterwards.
func redirectCheckStdout(stdout *os.File) func() {
	if vlog.IsQuiet() {
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err == nil {
			os.Stdout = devNull
			restored := false
			return func() {
				if restored {
					return
				}
				restored = true
				os.Stdout = stdout
				devNull.Close()
			}
		}
		return func() {}
	}
	os.Stdout = os.Stderr
	restored := false
	return func() {
		if restored {
			return
		}
		restored = true
		os.Stdout = stdout
	}
}

func emitCheckResult(stdout *os.File, result *BuildResult, runErr error) error {
	if runErr != nil {
		diagnostics, summary := buildFailureDetails(runErr)
		if len(diagnostics) == 0 {
			diagnostics = []buildDiagnostic{{Severity: "error", Code: "VMAKE_E_BUILD", Message: summary}}
		}
		if jsonOutput {
			jsonEnvelopeWritten = true
			document := &checkJSONEnvelope{
				SchemaVersion: jsonOutputSchemaVersion,
				Command:       reportCommandCheck,
				Status:        "error",
				Diagnostics:   diagnostics,
			}
			if writeErr := writeJSONDocument(stdout, document, outputFlag); writeErr != nil {
				return errors.Join(runErr, writeErr)
			}
			return runErr
		}
		fmt.Fprintf(stdout, "Build check failed: %s\n", summary)
		return runErr
	}
	if jsonOutput {
		stats := result.Stats
		projectRevision, projectDirty := projectProvenance(findProjectDir())
		jsonEnvelopeWritten = true
		document := &checkJSONEnvelope{
			SchemaVersion: jsonOutputSchemaVersion,
			Command:       reportCommandCheck,
			Status:        "ok",
			Data: &checkJSONData{
				WorkPending:       stats.PlannedCompiles > 0 || stats.PlannedLinks > 0,
				PlannedCompiles:   stats.PlannedCompiles,
				PlannedLinks:      stats.PlannedLinks,
				PlannedCallbacks:  stats.PlannedCallbacks,
				PlannedGenerators: stats.PlannedGenerators,
				Caveats:           checkCaveats(stats.PlannedGenerators),
				UpToDate:          stats.UpToDate,
				TargetsTotal:      len(result.Targets),
				ProjectRevision:   projectRevision,
				ProjectDirty:      projectDirty,
			},
			Diagnostics: []buildDiagnostic{},
		}
		return writeJSONDocument(stdout, document, outputFlag)
	}
	fmt.Fprintln(stdout, formatCheckSummary(result))
	return nil
}

func checkCaveats(generators int) []string {
	if generators == 0 {
		return nil
	}
	return []string{"generator rules/config headers are not executed; targets depending on generated inputs may be undercounted"}
}

func formatCheckSummary(result *BuildResult) string {
	stats := result.Stats
	pending := make([]string, 0, 3)
	if stats.PlannedCompiles > 0 {
		pending = append(pending, countLabel(stats.PlannedCompiles, "compile", "compiles"))
	}
	if stats.PlannedLinks > 0 {
		pending = append(pending, countLabel(stats.PlannedLinks, "link", "links"))
	}
	var summary string
	if len(pending) > 0 {
		if stats.PlannedCallbacks > 0 {
			pending = append(pending, countLabel(stats.PlannedCallbacks, "callback", "callbacks"))
		}
		summary = fmt.Sprintf("Build check: %s pending (%s up to date)", strings.Join(pending, ", "), countLabel(stats.UpToDate, "action", "actions"))
	} else if stats.PlannedCallbacks > 0 {
		summary = fmt.Sprintf("Build check: up to date (no compile/link work; %s would run)", countLabel(stats.PlannedCallbacks, "callback", "callbacks"))
	} else {
		summary = "Build check: up to date (no compile/link work)"
	}
	if stats.PlannedGenerators > 0 {
		summary += fmt.Sprintf("; %d generated-input target(s) not checked", stats.PlannedGenerators)
	}
	return summary
}

func countLabel(count int, singular, plural string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
}

func withResolvedProject(execution *RuntimeContext, run func(*RuntimeContext) error) error {
	return withBuildContext(execution, func() error {
		var mf *installManifest
		if manifestFlag != "" {
			var err error
			mf, err = loadInstallManifest(manifestFlag)
			if err != nil {
				return err
			}
			if err := importManifestIntoLock(execution.Context, mf); err != nil {
				return err
			}
		}
		ctx, err := resolveToConfigContext(execution.Context, false)
		if err != nil {
			return err
		}
		if ctx.Resolver != nil {
			defer ctx.Resolver.Close()
		}
		if mf != nil {
			if err := checkoutManifestLocals(ctx, mf); err != nil {
				return err
			}
		}
		return run(ctx)
	})
}

func runReportedBuild(ctx *RuntimeContext, report *buildReport, opts BuildOptions) (*BuildResult, error) {
	if err := report.bind(ctx); err != nil {
		return nil, err
	}
	if installFlag {
		projectDir := ""
		if ctx.Paths != nil {
			projectDir = ctx.Paths.ProjectDir
		}
		prefix := prefixFlag
		if prefix == "" && projectDir != "" {
			prefix = filepath.Join(projectDir, "install")
		}
		if err := validateInstallPrefix(prefix, projectDir, installStorageRoots(ctx)...); err != nil {
			return nil, err
		}
	}
	result, err := runBuildPhase(ctx, opts)
	if err != nil {
		if result != nil {
			report.partial(ctx, result, err)
		}
		return nil, err
	}
	if installFlag {
		if err := executeInstall(ctx, result); err != nil {
			return nil, err
		}
	}
	if err := report.complete(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func loadInstallManifest(path string) (*installManifest, error) {
	var mf installManifest
	if err := jsonio.Load(path, &mf); err != nil {
		return nil, err
	}
	return &mf, nil
}

// importManifestIntoLock pins remote package versions from an install manifest
// into vmake.lock BEFORE dependency resolution, so the graph is built from the
// pinned versions (not from latest-matching tags).
func importManifestIntoLock(ctx context.Context, mf *installManifest) error {
	lockPath := getLockfilePath()
	l, err := lockfile.LoadOrCreate(lockPath)
	if err != nil {
		return err
	}
	changed := false
	for _, entry := range mf.Packages {
		switch entry.Source {
		case "native", "registry":
			if entry.Ref == "" {
				vlog.Info("  skip manifest entry %s (empty ref)", entry.Name)
				continue
			}
			version := entry.Version
			if version == "" {
				version = entry.Ref
			}
			if entry.URL == "" {
				return fmt.Errorf("manifest entry %s has no URL; cannot resolve commit for %s", entry.Name, entry.Ref)
			}
			vlog.Info("  %s: resolving %s from %s", entry.Name, entry.Ref, entry.URL)
			commit, err := repo.ResolveRemoteCommitContext(ctx, entry.URL, entry.Ref)
			if err != nil {
				return fmt.Errorf("resolve commit for %s@%s: %w", entry.Name, entry.Ref, err)
			}
			l.Set(entry.Name, &lockfile.LockedPkg{
				Version: version,
				Commit:  commit,
				Source:  entry.Source,
			})
			vlog.Info("  pin %s -> %s (%s, commit %s)", entry.Name, version, entry.Source, repo.ShortCommit(commit))
			changed = true
		}
	}
	if changed {
		return l.Save(lockPath)
	}
	return nil
}

// checkoutManifestLocals checks out local packages to the refs recorded in an
// install manifest. Local packages with a managed git source are skipped: the
// manifest ref belongs to the upstream clone, while the checkout would run
// against the project repository, and the build materializes the recorded
// source version on its own.
func checkoutManifestLocals(ctx *RuntimeContext, mf *installManifest) error {
	base, err := manifestProjectDir(ctx)
	if err != nil {
		return err
	}

	for _, entry := range mf.Packages {
		if entry.Source != "local" {
			continue
		}
		if entry.Ref == "" || entry.Ref == "unknown" || entry.Path == "" {
			continue
		}
		target, err := resolveManifestEntryPath(base, entry.Path)
		if err != nil {
			return err
		}
		node := localManifestNode(ctx, entry.Name)
		if node == nil {
			return fmt.Errorf("manifest local package %q not found in the project graph", entry.Name)
		}
		if !node.IsLocal() {
			return fmt.Errorf("manifest package %q is not a local package", entry.Name)
		}
		if len(node.Pkg.GitURLs()) > 0 {
			vlog.Info("  skip checkout %s (managed git source)", entry.Name)
			continue
		}
		if node.Source == nil {
			return fmt.Errorf("manifest local package %q has no source directory", entry.Name)
		}
		sourceDir, err := filepath.Abs(node.Source.Dir)
		if err != nil {
			return fmt.Errorf("resolve manifest source for %s: %w", entry.Name, err)
		}
		if target != canonicalInstallPath(sourceDir) {
			return fmt.Errorf("manifest path %q does not match local package %q source directory", entry.Path, entry.Name)
		}
		if err := repo.CheckoutContext(ctx.Context, target, entry.Ref); err != nil {
			return err
		}
		shortRef := repo.ShortCommit(entry.Ref)
		if shortRef != entry.Ref {
			shortRef += "..."
		}
		vlog.Info("  checkout %s -> %s", entry.Name, shortRef)
	}
	return nil
}

func manifestProjectDir(ctx *RuntimeContext) (string, error) {
	if ctx != nil && ctx.Paths != nil && ctx.Paths.ProjectDir != "" {
		return ctx.Paths.ProjectDir, nil
	}
	if ctx != nil && ctx.WorkDir != "" {
		return ctx.WorkDir, nil
	}
	return os.Getwd()
}

// resolveManifestEntryPath resolves a manifest path against the project root
// and rejects paths that escape it. A path may contain ".." as long as the
// resolved location remains inside the project.
func resolveManifestEntryPath(projectDir, entryPath string) (string, error) {
	if strings.TrimSpace(entryPath) == "" {
		return "", fmt.Errorf("manifest path is empty")
	}
	if filepath.IsAbs(entryPath) {
		return "", fmt.Errorf("manifest path %q must be relative to the project root", entryPath)
	}
	if filepath.VolumeName(entryPath) != "" {
		return "", fmt.Errorf("manifest path %q must not contain a volume", entryPath)
	}
	absolute, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("resolve project root %q: %w", projectDir, err)
	}
	base := canonicalInstallPath(absolute)
	target := canonicalInstallPath(filepath.Join(absolute, entryPath))
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("manifest path %q escapes the project root", entryPath)
	}
	return target, nil
}

func localManifestNode(ctx *RuntimeContext, name string) *resolver.PackageNode {
	if ctx == nil || ctx.DepGraph == nil {
		return nil
	}
	node := ctx.DepGraph.Packages[name]
	if node == nil || node.Pkg == nil {
		return nil
	}
	return node
}
