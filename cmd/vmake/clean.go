package main

import (
	"os"

	vlog "gitee.com/spock2300/vmake/pkg/log"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean build artifacts",
	Long:  `Remove object files and build cache for all packages.`,
	Run:   runClean,
}

func init() {
	RootCmd.AddCommand(cleanCmd)
}

func runClean(cmd *cobra.Command, args []string) {
	ctx, err := PrepareBuild()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}

	origDir, _ := os.Getwd()

	for _, pkg := range ctx.Packages {
		if err := os.Chdir(pkg.Dir); err != nil {
			vlog.Error("Failed to chdir to %s: %v", pkg.Name, err)
			continue
		}

		objectsDir := "build/objects"
		if _, err := os.Stat(objectsDir); err == nil {
			if err := os.RemoveAll(objectsDir); err != nil {
				vlog.Error("Failed to clean %s: %v", pkg.Name, err)
				os.Chdir(origDir)
				continue
			}
			vlog.Info("Cleaned %s/build/objects/", pkg.Name)
		}

		cachePath := "build/cache.json"
		os.Remove(cachePath)
	}

	os.Chdir(origDir)
	vlog.Info("Clean completed!")
}
