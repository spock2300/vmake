package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the project",
	Long:  `Compile and link all targets defined in build.go files.`,
	Run:   runBuild,
}

func init() {
	RootCmd.AddCommand(buildCmd)
	addInstallFlags(buildCmd)
	addBuildFlags(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) {
	ctx := resolveToConfig()
	if manifestFlag != "" {
		applyManifestVersions(ctx, manifestFlag)
	}
	result, err := runBuildPhase(ctx, testsFlag)
	fatalErr(err)
	if installFlag {
		fatalErr(executeInstall(ctx, result))
	}
}

func applyManifestVersions(ctx *RuntimeContext, manifestPath string) {
	var mf installManifest
	fatalErr(jsonio.Load(manifestPath, &mf))

	cwd, err := os.Getwd()
	fatalErr(err)

	for _, entry := range mf.Packages {
		switch entry.Source {
		case "local":
			if entry.Ref == "" || entry.Ref == "unknown" {
				continue
			}
			fatalErr(repo.Checkout(filepath.Join(cwd, entry.Path), entry.Ref))
			shortRef := entry.Ref
			if len(shortRef) > 12 {
				shortRef = shortRef[:12]
			}
			vlog.Info("  checkout %s -> %s", entry.Name, shortRef+"...")
		case "native", "registry":
			if entry.Ref == "" {
				continue
			}
			existing := config.GetEntry(ctx.Config, entry.Name)
			existing.Version = entry.Ref
			config.SetEntry(ctx.Config, entry.Name, existing)
			vlog.Info("  pin %s -> %s", entry.Name, entry.Ref)
		}
	}
}
