package main

import (
	vlog "github.com/spock2300/vmake/pkg/log"

	"github.com/spf13/cobra"
)

var rebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild the project",
	Long:  `Clean and then build the project from scratch.`,
	RunE:  runRebuild,
}

func init() {
	RootCmd.AddCommand(rebuildCmd)
	addInstallFlags(rebuildCmd)
	addBuildFlags(rebuildCmd)
}

func runRebuild(cmd *cobra.Command, args []string) (err error) {
	commandStorageLocks()
	report, err := beginBuildReport()
	if err != nil {
		return err
	}
	defer report.finish(&err)
	execution := &RuntimeContext{}
	return withBuildContext(execution, func() error {
		if manifestFlag != "" {
			if err := importManifestIntoLock(execution.Context, manifestFlag); err != nil {
				return err
			}
		}
		ctx, err := resolveToConfigContext(execution.Context, false)
		if err != nil {
			return err
		}
		if manifestFlag != "" {
			if err := checkoutManifestLocals(ctx, manifestFlag); err != nil {
				return err
			}
		}
		executeCleanLocal(ctx)
		vlog.Info("")
		if err := report.bind(ctx); err != nil {
			return err
		}
		result, err := runBuildPhase(ctx, BuildOptions{IncludeTests: testsFlag, Jobs: jobsFlag, KeepGoing: keepGoingFlag})
		if err != nil {
			return err
		}
		if installFlag {
			if err := executeInstall(ctx, result); err != nil {
				return err
			}
		}
		return report.complete(ctx, result)
	})
}

func executeCleanLocal(ctx *RuntimeContext) {
	vlog.Info("")
	vlog.Info("Executing OnClean...")
	if err := executeCleanHooks(ctx, true, true); err != nil {
		vlog.Error("Skipping OnClean: %v", err)
	}
	if err := cleanPackages(collectCleanEntries(ctx), ctx, false); err != nil {
		vlog.Error("Error: %v", err)
	}
}
