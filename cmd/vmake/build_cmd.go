package main

import (
	"context"
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
	Long:  `Compile and link all targets defined in build.go files.`,
	RunE:  runBuild,
}

func init() {
	RootCmd.AddCommand(buildCmd)
	addInstallFlags(buildCmd)
	addBuildFlags(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	commandStorageLocks()
	return withBuildReport(func(report *buildReport) error {
		execution := &RuntimeContext{}
		return withResolvedProject(execution, func(ctx *RuntimeContext) error {
			_, err := runReportedBuild(ctx, report, BuildOptions{IncludeTests: testsFlag, Jobs: jobsFlag, KeepGoing: keepGoingFlag, Explain: explainFlag})
			return err
		})
	})
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
