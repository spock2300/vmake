package main

import (
	"os"
	"path/filepath"

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

	for _, pkg := range ctx.Packages {
		buildDir := filepath.Join(filepath.Dir(pkg.Path), "build")
		objectsDir := filepath.Join(buildDir, "objects")

		if _, err := os.Stat(objectsDir); err == nil {
			if err := os.RemoveAll(objectsDir); err != nil {
				vlog.Error("Failed to clean %s: %v", pkg.Name, err)
				continue
			}
			vlog.Info("Cleaned %s/objects/", pkg.Name)
		}

		cachePath := filepath.Join(buildDir, "cache.json")
		os.Remove(cachePath)
	}

	vlog.Info("Clean completed!")
}
