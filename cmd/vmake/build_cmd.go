package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/pipeline"
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
		if lockedFlag && manifestFlag != "" {
			return fmt.Errorf("--locked cannot be combined with --manifest")
		}
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
		ctx, err := openProject(projectOptions{execution: execution.Context})
		if err != nil {
			return err
		}
		defer closeProject(ctx)
		if mf != nil {
			if err := checkoutManifestLocals(ctx, mf); err != nil {
				return err
			}
		}
		return run(ctx)
	})
}

func runReportedBuild(ctx *RuntimeContext, report *buildReport, opts BuildOptions) (*BuildResult, error) {
	if err := report.Bind(ctx); err != nil {
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
			report.Partial(ctx, result, err)
		}
		return nil, err
	}
	if installFlag {
		if err := executeInstall(ctx, result); err != nil {
			return nil, err
		}
	}
	if err := report.Complete(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func loadInstallManifest(path string) (*installManifest, error) {
	return pipeline.LoadInstallManifest(path)
}

func importManifestIntoLock(ctx context.Context, mf *installManifest) error {
	return pipeline.ImportManifestIntoLock(ctx, findProjectDir(), getLockfilePath(), mf)
}

func checkoutManifestLocals(ctx *RuntimeContext, mf *installManifest) error {
	return pipeline.CheckoutManifestLocals(ctx, mf)
}

func manifestProjectDir(ctx *RuntimeContext) (string, error) {
	return pipeline.ManifestProjectDir(ctx)
}

func resolveManifestEntryPath(projectDir, entryPath string) (string, error) {
	return pipeline.ResolveManifestEntryPath(projectDir, entryPath)
}
